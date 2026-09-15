package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

// ifdTarget says which IFD a tag's value belongs in when it's written
// back out.
type ifdTarget int

const (
	ifd0 ifdTarget = iota
	ifdExif
	ifdGPS
)

// Field types for the tags in ifd0TagNames and exifTagNames, keyed by
// tag ID. Needed to encode a sidecar value back into the right binary
// shape; decodeValue doesn't need this because the type comes off the
// wire along with the value.
var ifd0TagTypes = map[uint16]uint16{
	0x010E: typeASCII,
	0x010F: typeASCII,
	0x0110: typeASCII,
	0x0112: typeShort,
	0x011A: typeRational,
	0x011B: typeRational,
	0x0128: typeShort,
	0x0131: typeASCII,
	0x0132: typeASCII,
	0x013B: typeASCII,
	0x0213: typeShort,
	0x8298: typeASCII,
}

var exifTagTypes = map[uint16]uint16{
	0x829A: typeRational,
	0x829D: typeRational,
	0x8822: typeShort,
	0x8827: typeShort,
	0x9000: typeUndefined,
	0x9003: typeASCII,
	0x9004: typeASCII,
	0x9101: typeUndefined,
	0x9201: typeSRational,
	0x9202: typeRational,
	0x9204: typeSRational,
	0x9205: typeRational,
	0x9207: typeShort,
	0x9209: typeShort,
	0x920A: typeRational,
	0xA002: typeLong,
	0xA003: typeLong,
	0xA402: typeShort,
	0xA403: typeShort,
	0xA406: typeShort,
	0xA420: typeASCII,
}

var gpsTagTypes = map[uint16]uint16{
	0x0000: typeByte,
	0x0001: typeASCII,
	0x0002: typeRational,
	0x0003: typeASCII,
	0x0004: typeRational,
	0x0005: typeByte,
	0x0006: typeRational,
	0x0007: typeRational,
	0x0008: typeASCII,
	0x0009: typeASCII,
	0x000A: typeASCII,
	0x000B: typeRational,
	0x000C: typeASCII,
	0x000D: typeRational,
	0x000E: typeASCII,
	0x000F: typeRational,
	0x0010: typeASCII,
	0x0011: typeRational,
	0x0012: typeASCII,
	0x0013: typeASCII,
	0x0014: typeRational,
	0x0015: typeASCII,
	0x0016: typeRational,
	0x0017: typeASCII,
	0x0018: typeRational,
	0x0019: typeASCII,
	0x001A: typeRational,
	0x001B: typeUndefined,
	0x001C: typeUndefined,
	0x001D: typeASCII,
	0x001E: typeShort,
}

var ifd0NameToID = reverseTagNames(ifd0TagNames)
var exifNameToID = reverseTagNames(exifTagNames)
var gpsNameToID = reverseTagNames(gpsTagNames)

func reverseTagNames(m map[uint16]string) map[string]uint16 {
	r := make(map[string]uint16, len(m))
	for id, name := range m {
		r[name] = id
	}
	return r
}

// resolveTagName looks up a sidecar tag name against the known IFD0 and
// Exif SubIFD tags.
func resolveTagName(name string) (id uint16, typ uint16, target ifdTarget, ok bool) {
	if tid, exists := ifd0NameToID[name]; exists {
		if t, exists2 := ifd0TagTypes[tid]; exists2 {
			return tid, t, ifd0, true
		}
	}
	if tid, exists := exifNameToID[name]; exists {
		if t, exists2 := exifTagTypes[tid]; exists2 {
			return tid, t, ifdExif, true
		}
	}
	if tid, exists := gpsNameToID[name]; exists {
		if t, exists2 := gpsTagTypes[tid]; exists2 {
			return tid, t, ifdGPS, true
		}
	}
	return 0, 0, 0, false
}

// parseUnknownTagName reverses the "Tag0x...", "ExifTag0x...", and
// "GPSTag0x..." fallback names readIFD gives tags it has no name for.
// The prefix is what lets a tag like MakerNote (Exif SubIFD, not in
// exifTagNames) go back into the IFD it actually came from instead of
// always landing in IFD0. Unknown tags carry no type information once
// they've gone through the sidecar, so they're encoded as raw bytes.
func parseUnknownTagName(name string) (id uint16, typ uint16, target ifdTarget, err error) {
	var prefix string
	switch {
	case strings.HasPrefix(name, "ExifTag0x"):
		prefix, target = "ExifTag0x", ifdExif
	case strings.HasPrefix(name, "GPSTag0x"):
		prefix, target = "GPSTag0x", ifdGPS
	case strings.HasPrefix(name, "Tag0x"):
		prefix, target = "Tag0x", ifd0
	default:
		return 0, 0, 0, fmt.Errorf("unrecognized tag name %q", name)
	}

	n, err := strconv.ParseUint(name[len(prefix):], 16, 16)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("unrecognized tag name %q", name)
	}
	return uint16(n), typeUndefined, target, nil
}

type encodedEntry struct {
	id    uint16
	typ   uint16
	count uint32
	data  []byte
}

// BuildEXIF turns sidecar tags back into a TIFF-format EXIF blob: IFD0,
// plus an Exif SubIFD and a GPSInfo IFD if tags for either are present.
// The IFD1 thumbnail directory isn't produced yet.
func BuildEXIF(tags []Tag) ([]byte, error) {
	bo := binary.BigEndian
	var ifd0Entries, exifEntries, gpsEntries []encodedEntry

	for _, t := range tags {
		// These are derived from the IFD structure itself (the Exif
		// SubIFD offset is recomputed below; GPS isn't rebuilt yet),
		// not values a sidecar edit should feed back in.
		if t.Name == "ExifIFDPointer" || t.Name == "GPSInfoIFDPointer" {
			continue
		}

		id, typ, target, ok := resolveTagName(t.Name)
		if !ok {
			var err error
			id, typ, target, err = parseUnknownTagName(t.Name)
			if err != nil {
				return nil, err
			}
		}

		data, count, err := encodeValue(bo, typ, t.Value)
		if err != nil {
			return nil, fmt.Errorf("tag %q: %w", t.Name, err)
		}

		entry := encodedEntry{id: id, typ: typ, count: count, data: data}
		switch target {
		case ifdExif:
			exifEntries = append(exifEntries, entry)
		case ifdGPS:
			gpsEntries = append(gpsEntries, entry)
		default:
			ifd0Entries = append(ifd0Entries, entry)
		}
	}

	if len(ifd0Entries) == 0 && len(exifEntries) == 0 && len(gpsEntries) == 0 {
		return nil, errors.New("no tags to embed")
	}

	if len(exifEntries) > 0 {
		ifd0Entries = append(ifd0Entries, encodedEntry{id: exifIFDPointerTag, typ: typeLong, count: 1, data: make([]byte, 4)})
	}
	if len(gpsEntries) > 0 {
		ifd0Entries = append(ifd0Entries, encodedEntry{id: gpsIFDPointerTag, typ: typeLong, count: 1, data: make([]byte, 4)})
	}

	header := make([]byte, 8)
	copy(header, "MM")
	bo.PutUint16(header[2:4], 42)
	bo.PutUint32(header[4:8], 8)

	ifd0Dir, ifd0Overflow := buildIFDBlock(bo, ifd0Entries, 8, 0)

	var exifDir, exifOverflow []byte
	if len(exifEntries) > 0 {
		exifDirOffset := uint32(8 + len(ifd0Dir) + len(ifd0Overflow))
		patchEntryValue(ifd0Dir, bo, exifIFDPointerTag, exifDirOffset)
		exifDir, exifOverflow = buildIFDBlock(bo, exifEntries, exifDirOffset, 0)
	}

	var gpsDir, gpsOverflow []byte
	if len(gpsEntries) > 0 {
		gpsDirOffset := uint32(8 + len(ifd0Dir) + len(ifd0Overflow) + len(exifDir) + len(exifOverflow))
		patchEntryValue(ifd0Dir, bo, gpsIFDPointerTag, gpsDirOffset)
		gpsDir, gpsOverflow = buildIFDBlock(bo, gpsEntries, gpsDirOffset, 0)
	}

	out := make([]byte, 0, len(header)+len(ifd0Dir)+len(ifd0Overflow)+len(exifDir)+len(exifOverflow)+len(gpsDir)+len(gpsOverflow))
	out = append(out, header...)
	out = append(out, ifd0Dir...)
	out = append(out, ifd0Overflow...)
	out = append(out, exifDir...)
	out = append(out, exifOverflow...)
	out = append(out, gpsDir...)
	out = append(out, gpsOverflow...)
	return out, nil
}

// buildIFDBlock lays out one IFD's directory (sorted by tag ID, as the
// TIFF spec requires) and the overflow area holding values too big to
// fit inline. dirOffset is this IFD's own offset within the TIFF blob,
// needed to compute where its overflow area starts.
func buildIFDBlock(bo binary.ByteOrder, entries []encodedEntry, dirOffset uint32, next uint32) (dir []byte, overflow []byte) {
	sorted := make([]encodedEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].id < sorted[j].id })

	count := len(sorted)
	dirSize := 2 + count*12 + 4
	dir = make([]byte, dirSize)
	bo.PutUint16(dir[0:2], uint16(count))

	overflowOffset := dirOffset + uint32(dirSize)
	for i, e := range sorted {
		base := 2 + i*12
		bo.PutUint16(dir[base:base+2], e.id)
		bo.PutUint16(dir[base+2:base+4], e.typ)
		bo.PutUint32(dir[base+4:base+8], e.count)
		if len(e.data) <= 4 {
			copy(dir[base+8:base+12], e.data)
			continue
		}
		bo.PutUint32(dir[base+8:base+12], overflowOffset)
		overflow = append(overflow, e.data...)
		overflowOffset += uint32(len(e.data))
		if len(e.data)%2 == 1 {
			// Keep later values on an even offset, as TIFF readers expect.
			overflow = append(overflow, 0)
			overflowOffset++
		}
	}
	bo.PutUint32(dir[2+count*12:], next)
	return dir, overflow
}

func patchEntryValue(dir []byte, bo binary.ByteOrder, id uint16, value uint32) {
	count := int(bo.Uint16(dir[0:2]))
	for i := 0; i < count; i++ {
		base := 2 + i*12
		if bo.Uint16(dir[base:base+2]) == id {
			bo.PutUint32(dir[base+8:base+12], value)
			return
		}
	}
}

// encodeValue is the reverse of decodeValue: it turns a sidecar value
// string back into the raw bytes for a TIFF field of the given type.
func encodeValue(bo binary.ByteOrder, typ uint16, value string) (data []byte, count uint32, err error) {
	switch typ {
	case typeASCII:
		data = append([]byte(value), 0)
		return data, uint32(len(data)), nil

	case typeShort, typeSShort:
		parts := splitValues(value)
		data = make([]byte, len(parts)*2)
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if typ == typeShort {
				n, e := strconv.ParseUint(p, 10, 16)
				if e != nil {
					return nil, 0, fmt.Errorf("invalid SHORT value %q: %w", p, e)
				}
				bo.PutUint16(data[i*2:], uint16(n))
			} else {
				n, e := strconv.ParseInt(p, 10, 16)
				if e != nil {
					return nil, 0, fmt.Errorf("invalid SSHORT value %q: %w", p, e)
				}
				bo.PutUint16(data[i*2:], uint16(int16(n)))
			}
		}
		return data, uint32(len(parts)), nil

	case typeLong, typeSLong:
		parts := splitValues(value)
		data = make([]byte, len(parts)*4)
		for i, p := range parts {
			p = strings.TrimSpace(p)
			if typ == typeLong {
				n, e := strconv.ParseUint(p, 10, 32)
				if e != nil {
					return nil, 0, fmt.Errorf("invalid LONG value %q: %w", p, e)
				}
				bo.PutUint32(data[i*4:], uint32(n))
			} else {
				n, e := strconv.ParseInt(p, 10, 32)
				if e != nil {
					return nil, 0, fmt.Errorf("invalid SLONG value %q: %w", p, e)
				}
				bo.PutUint32(data[i*4:], uint32(int32(n)))
			}
		}
		return data, uint32(len(parts)), nil

	case typeRational, typeSRational:
		parts := splitValues(value)
		data = make([]byte, len(parts)*8)
		for i, p := range parts {
			num, den, e := parseRational(strings.TrimSpace(p))
			if e != nil {
				return nil, 0, e
			}
			if typ == typeRational {
				if num < 0 || den < 0 || num > math.MaxUint32 || den > math.MaxUint32 {
					return nil, 0, fmt.Errorf("RATIONAL value %q out of range", p)
				}
				bo.PutUint32(data[i*8:], uint32(num))
				bo.PutUint32(data[i*8+4:], uint32(den))
			} else {
				if num < math.MinInt32 || num > math.MaxInt32 || den < math.MinInt32 || den > math.MaxInt32 {
					return nil, 0, fmt.Errorf("SRATIONAL value %q out of range", p)
				}
				bo.PutUint32(data[i*8:], uint32(int32(num)))
				bo.PutUint32(data[i*8+4:], uint32(int32(den)))
			}
		}
		return data, uint32(len(parts)), nil

	default: // BYTE, SBYTE, UNDEFINED: sidecar holds them as a hex string.
		raw, e := hex.DecodeString(strings.TrimSpace(value))
		if e != nil {
			return nil, 0, fmt.Errorf("invalid hex value %q: %w", value, e)
		}
		return raw, uint32(len(raw)), nil
	}
}

// splitValues undoes the ", " join that decodeValue uses for multi-value
// fields like ISOSpeedRatings.
func splitValues(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, ", ")
}

// parseRational accepts either a plain "a/b" fraction or a decimal
// number (what formatRational actually produces today) and returns it
// as an exact numerator/denominator pair.
func parseRational(s string) (num, den int64, err error) {
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return 0, 0, fmt.Errorf("invalid rational value %q", s)
	}
	if !r.Num().IsInt64() || !r.Denom().IsInt64() {
		return 0, 0, fmt.Errorf("rational value %q is too large", s)
	}
	return r.Num().Int64(), r.Denom().Int64(), nil
}

// buildAPP1 wraps a TIFF blob in a JPEG APP1 marker segment with the
// Exif header the segment needs to be recognized as EXIF data.
func buildAPP1(tiff []byte) ([]byte, error) {
	length := len(tiff) + 8 // "Exif\x00\x00" + the 2-byte length field itself
	if length > 0xFFFF {
		return nil, fmt.Errorf("EXIF data too large for a single APP1 segment (%d bytes)", len(tiff))
	}
	seg := make([]byte, 0, 2+length)
	seg = append(seg, 0xFF, 0xE1)
	seg = append(seg, byte(length>>8), byte(length))
	seg = append(seg, "Exif\x00\x00"...)
	seg = append(seg, tiff...)
	return seg, nil
}

// EmbedEXIF returns a copy of a JPEG file with tiff (a TIFF-format EXIF
// blob, as returned by BuildEXIF) embedded as its APP1 Exif segment. An
// existing Exif segment is replaced in place; otherwise the new segment
// is inserted right after the SOI marker.
func EmbedEXIF(jpegData []byte, tiff []byte) ([]byte, error) {
	segment, err := buildAPP1(tiff)
	if err != nil {
		return nil, err
	}

	start, end, err := findEXIFSegmentSpan(jpegData)
	if err != nil {
		return nil, err
	}

	if start < 0 {
		out := make([]byte, 0, len(jpegData)+len(segment))
		out = append(out, jpegData[:2]...)
		out = append(out, segment...)
		out = append(out, jpegData[2:]...)
		return out, nil
	}

	out := make([]byte, 0, len(jpegData)-(end-start)+len(segment))
	out = append(out, jpegData[:start]...)
	out = append(out, segment...)
	out = append(out, jpegData[end:]...)
	return out, nil
}
