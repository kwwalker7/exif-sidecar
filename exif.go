package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Tag is a single EXIF entry, in the order it was read from the file.
type Tag struct {
	Name  string
	Value string
}

// TIFF field types, as defined by the EXIF/TIFF spec.
const (
	typeByte      = 1
	typeASCII     = 2
	typeShort     = 3
	typeLong      = 4
	typeRational  = 5
	typeSByte     = 6
	typeUndefined = 7
	typeSShort    = 8
	typeSLong     = 9
	typeSRational = 10
	typeFloat     = 11
	typeDouble    = 12
)

var typeSizes = map[uint16]uint32{
	typeByte: 1, typeASCII: 1, typeShort: 2, typeLong: 4, typeRational: 8,
	typeSByte: 1, typeUndefined: 1, typeSShort: 2, typeSLong: 4, typeSRational: 8,
	typeFloat: 4, typeDouble: 8,
}

// Tag ID pointing at the Exif SubIFD, which holds camera-specific fields
// (exposure, ISO, capture time) that don't live in IFD0.
const exifIFDPointerTag = 0x8769

// Tag ID pointing at the GPSInfo IFD, which holds location fields.
const gpsIFDPointerTag = 0x8825

// Tag IDs in IFD1 (the thumbnail directory) that together locate the
// thumbnail's own JPEG bytes elsewhere in the TIFF blob: an offset and a
// byte count. They're surfaced as a single "ThumbnailImage" tag instead
// of the raw offset/length pair, since the offset is meaningless once
// the tags are outside the file.
const jpegInterchangeFormatTag = 0x0201
const jpegInterchangeFormatLengthTag = 0x0202

var ifd0TagNames = map[uint16]string{
	0x010E: "ImageDescription",
	0x010F: "Make",
	0x0110: "Model",
	0x0112: "Orientation",
	0x011A: "XResolution",
	0x011B: "YResolution",
	0x0128: "ResolutionUnit",
	0x0131: "Software",
	0x0132: "DateTime",
	0x013B: "Artist",
	0x0213: "YCbCrPositioning",
	0x8298: "Copyright",
	0x8769: "ExifIFDPointer",
	0x8825: "GPSInfoIFDPointer",
}

var exifTagNames = map[uint16]string{
	0x829A: "ExposureTime",
	0x829D: "FNumber",
	0x8822: "ExposureProgram",
	0x8827: "ISOSpeedRatings",
	0x9000: "ExifVersion",
	0x9003: "DateTimeOriginal",
	0x9004: "DateTimeDigitized",
	0x9101: "ComponentsConfiguration",
	0x9201: "ShutterSpeedValue",
	0x9202: "ApertureValue",
	0x9204: "ExposureBiasValue",
	0x9205: "MaxApertureValue",
	0x9207: "MeteringMode",
	0x9209: "Flash",
	0x920A: "FocalLength",
	0xA002: "PixelXDimension",
	0xA003: "PixelYDimension",
	0xA402: "ExposureMode",
	0xA403: "WhiteBalance",
	0xA406: "SceneCaptureType",
	0xA420: "ImageUniqueID",
}

// ifd1TagNames covers the IFD1 fields that accompany a compressed
// (JPEG) thumbnail, which is what cameras and phones actually write.
// The rarer uncompressed strip-based thumbnail layout isn't handled;
// its tags fall through to the generic "ThumbnailTag0x..." naming.
var ifd1TagNames = map[uint16]string{
	0x0103: "ThumbnailCompression",
	0x011A: "ThumbnailXResolution",
	0x011B: "ThumbnailYResolution",
	0x0128: "ThumbnailResolutionUnit",
}

var gpsTagNames = map[uint16]string{
	0x0000: "GPSVersionID",
	0x0001: "GPSLatitudeRef",
	0x0002: "GPSLatitude",
	0x0003: "GPSLongitudeRef",
	0x0004: "GPSLongitude",
	0x0005: "GPSAltitudeRef",
	0x0006: "GPSAltitude",
	0x0007: "GPSTimeStamp",
	0x0008: "GPSSatellites",
	0x0009: "GPSStatus",
	0x000A: "GPSMeasureMode",
	0x000B: "GPSDOP",
	0x000C: "GPSSpeedRef",
	0x000D: "GPSSpeed",
	0x000E: "GPSTrackRef",
	0x000F: "GPSTrack",
	0x0010: "GPSImgDirectionRef",
	0x0011: "GPSImgDirection",
	0x0012: "GPSMapDatum",
	0x0013: "GPSDestLatitudeRef",
	0x0014: "GPSDestLatitude",
	0x0015: "GPSDestLongitudeRef",
	0x0016: "GPSDestLongitude",
	0x0017: "GPSDestBearingRef",
	0x0018: "GPSDestBearing",
	0x0019: "GPSDestDistanceRef",
	0x001A: "GPSDestDistance",
	0x001B: "GPSProcessingMethod",
	0x001C: "GPSAreaInformation",
	0x001D: "GPSDateStamp",
	0x001E: "GPSDifferential",
}

// ExtractEXIF reads a JPEG file's APP1 segment and returns the EXIF tags
// found in IFD0, the Exif SubIFD, the GPSInfo IFD, and IFD1 (thumbnail).
func ExtractEXIF(data []byte) ([]Tag, error) {
	tiff, err := findEXIFSegment(data)
	if err != nil {
		return nil, err
	}
	if len(tiff) < 8 {
		return nil, errors.New("EXIF data too short to contain a TIFF header")
	}

	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, fmt.Errorf("unrecognized TIFF byte order %q", tiff[:2])
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return nil, errors.New("invalid TIFF magic number")
	}

	var tags []Tag
	seen := map[uint32]bool{}
	offset := bo.Uint32(tiff[4:8])
	firstIFD := true
	for offset != 0 && !seen[offset] {
		seen[offset] = true
		// The first IFD in the chain is IFD0; any IFD after it is IFD1,
		// the thumbnail directory, which uses its own tag names.
		names, prefix := ifd0TagNames, ""
		if !firstIFD {
			names, prefix = ifd1TagNames, "Thumbnail"
		}
		entries, next, exifSubIFD, gpsIFD, thumbOffset, thumbLength, err := readIFD(tiff, bo, offset, names, prefix)
		if err != nil {
			return nil, err
		}
		tags = append(tags, entries...)

		if thumbOffset != 0 && thumbLength != 0 {
			end := uint64(thumbOffset) + uint64(thumbLength)
			if end > uint64(len(tiff)) {
				return nil, fmt.Errorf("thumbnail data at offset %d overruns EXIF data", thumbOffset)
			}
			tags = append(tags, Tag{Name: "ThumbnailImage", Value: hex.EncodeToString(tiff[thumbOffset:end])})
		}

		if exifSubIFD != 0 && !seen[exifSubIFD] {
			seen[exifSubIFD] = true
			// MakerNote (0x927C) and any other tag the maker didn't
			// register end up here; the "Exif" prefix on the fallback
			// name is what lets BuildEXIF put them back in the SubIFD
			// instead of IFD0.
			subEntries, _, _, _, _, _, err := readIFD(tiff, bo, exifSubIFD, exifTagNames, "Exif")
			if err != nil {
				return nil, err
			}
			tags = append(tags, subEntries...)
		}

		if gpsIFD != 0 && !seen[gpsIFD] {
			seen[gpsIFD] = true
			gpsEntries, _, _, _, _, _, err := readIFD(tiff, bo, gpsIFD, gpsTagNames, "GPS")
			if err != nil {
				return nil, err
			}
			tags = append(tags, gpsEntries...)
		}

		offset = next
		firstIFD = false
	}

	if len(tags) == 0 {
		return nil, errors.New("EXIF data contained no readable tags")
	}
	return tags, nil
}

// findEXIFSegment scans a JPEG's marker segments and returns the payload
// of the first APP1 segment that carries an Exif header, with that header
// stripped off (so what's returned starts at the TIFF header).
func findEXIFSegment(data []byte) ([]byte, error) {
	start, end, err := findEXIFSegmentSpan(data)
	if err != nil {
		return nil, err
	}
	if start < 0 {
		return nil, errors.New("no EXIF (APP1) segment found")
	}
	// start+2 (marker) +2 (length) +6 ("Exif\0\0") is where the TIFF header begins.
	return data[start+10 : end], nil
}

// findEXIFSegmentSpan scans a JPEG's marker segments and returns the byte
// range [start, end) of the first APP1 segment that carries an Exif
// header, start pointing at its 0xFF marker byte and end just past its
// payload. It returns start == -1 if the JPEG is well-formed but has no
// such segment.
func findEXIFSegmentSpan(data []byte) (start, end int, err error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return -1, -1, errors.New("not a JPEG file (missing SOI marker)")
	}

	pos := 2
	for pos+2 <= len(data) {
		if data[pos] != 0xFF {
			return -1, -1, fmt.Errorf("malformed JPEG: expected marker at offset %d", pos)
		}
		marker := data[pos+1]
		segStart := pos
		pos += 2

		// Markers with no length/payload of their own.
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if marker == 0xDA {
			// Start of scan: compressed image data follows, no more
			// marker segments to look at.
			break
		}
		if pos+2 > len(data) {
			break
		}

		segLen := int(data[pos])<<8 | int(data[pos+1])
		if segLen < 2 || pos+segLen > len(data) {
			return -1, -1, fmt.Errorf("malformed JPEG segment at offset %d", pos)
		}
		payload := data[pos+2 : pos+segLen]

		if marker == 0xE1 && len(payload) >= 6 && string(payload[:6]) == "Exif\x00\x00" {
			return segStart, pos + segLen, nil
		}
		pos += segLen
	}
	return -1, -1, nil
}

// readIFD parses one Image File Directory at offset, returning its tags,
// the offset of the next IFD in the chain (0 if none), the offsets of
// the Exif SubIFD and GPSInfo IFD if this directory pointed at either,
// and the offset/length pair from IFD1's JPEGInterchangeFormat tags if
// this directory pointed at a thumbnail image.
// unknownPrefix is prepended to the fallback "Tag0x..." name given to
// tags with no entry in names, so BuildEXIF can tell which IFD an
// unrecognized tag came from and put it back there.
func readIFD(tiff []byte, bo binary.ByteOrder, offset uint32, names map[uint16]string, unknownPrefix string) (tags []Tag, next uint32, exifSubIFD uint32, gpsIFD uint32, thumbOffset uint32, thumbLength uint32, err error) {
	if uint64(offset)+2 > uint64(len(tiff)) {
		return nil, 0, 0, 0, 0, 0, fmt.Errorf("IFD offset %d out of range", offset)
	}
	count := bo.Uint16(tiff[offset : offset+2])
	entriesStart := uint64(offset) + 2
	entriesEnd := entriesStart + uint64(count)*12
	if entriesEnd+4 > uint64(len(tiff)) {
		return nil, 0, 0, 0, 0, 0, fmt.Errorf("IFD at offset %d overruns EXIF data", offset)
	}

	for i := uint64(0); i < uint64(count); i++ {
		entry := tiff[entriesStart+i*12:]
		tag := bo.Uint16(entry[0:2])
		typ := bo.Uint16(entry[2:4])
		cnt := bo.Uint32(entry[4:8])

		size, ok := typeSizes[typ]
		if !ok {
			continue // unsupported / vendor-specific field type
		}
		total := uint64(size) * uint64(cnt)

		var raw []byte
		if total <= 4 {
			raw = entry[8 : 8+total]
		} else {
			valOffset := uint64(bo.Uint32(entry[8:12]))
			if valOffset+total > uint64(len(tiff)) {
				continue // value would read past the end of the buffer
			}
			raw = tiff[valOffset : valOffset+total]
		}

		if tag == exifIFDPointerTag && typ == typeLong && cnt == 1 {
			exifSubIFD = bo.Uint32(raw)
			continue
		}
		if tag == gpsIFDPointerTag && typ == typeLong && cnt == 1 {
			gpsIFD = bo.Uint32(raw)
			continue
		}
		if tag == jpegInterchangeFormatTag && typ == typeLong && cnt == 1 {
			thumbOffset = bo.Uint32(raw)
			continue
		}
		if tag == jpegInterchangeFormatLengthTag && typ == typeLong && cnt == 1 {
			thumbLength = bo.Uint32(raw)
			continue
		}

		name, known := names[tag]
		if !known {
			name = fmt.Sprintf("%sTag0x%04X", unknownPrefix, tag)
		}
		tags = append(tags, Tag{Name: name, Value: decodeValue(bo, typ, cnt, raw)})
	}

	next = bo.Uint32(tiff[entriesEnd : entriesEnd+4])
	return tags, next, exifSubIFD, gpsIFD, thumbOffset, thumbLength, nil
}

func decodeValue(bo binary.ByteOrder, typ uint16, count uint32, raw []byte) string {
	switch typ {
	case typeASCII:
		return strings.TrimRight(string(raw), "\x00")
	case typeShort:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = strconv.FormatUint(uint64(bo.Uint16(raw[i*2:])), 10)
		}
		return strings.Join(vals, ", ")
	case typeLong:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = strconv.FormatUint(uint64(bo.Uint32(raw[i*4:])), 10)
		}
		return strings.Join(vals, ", ")
	case typeSShort:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = strconv.FormatInt(int64(int16(bo.Uint16(raw[i*2:]))), 10)
		}
		return strings.Join(vals, ", ")
	case typeSLong:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = strconv.FormatInt(int64(int32(bo.Uint32(raw[i*4:]))), 10)
		}
		return strings.Join(vals, ", ")
	case typeRational:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			off := i * 8
			vals[i] = formatRational(int64(bo.Uint32(raw[off:])), int64(bo.Uint32(raw[off+4:])))
		}
		return strings.Join(vals, ", ")
	case typeSRational:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			off := i * 8
			vals[i] = formatRational(int64(int32(bo.Uint32(raw[off:]))), int64(int32(bo.Uint32(raw[off+4:]))))
		}
		return strings.Join(vals, ", ")
	case typeFloat:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = strconv.FormatFloat(float64(math.Float32frombits(bo.Uint32(raw[i*4:]))), 'g', -1, 32)
		}
		return strings.Join(vals, ", ")
	case typeDouble:
		vals := make([]string, count)
		for i := uint32(0); i < count; i++ {
			vals[i] = strconv.FormatFloat(math.Float64frombits(bo.Uint64(raw[i*8:])), 'g', -1, 64)
		}
		return strings.Join(vals, ", ")
	default: // BYTE, SBYTE, UNDEFINED
		strs := make([]string, len(raw))
		for i, b := range raw {
			strs[i] = fmt.Sprintf("%02X", b)
		}
		return strings.Join(strs, "")
	}
}

func formatRational(num, den int64) string {
	if den == 0 {
		return fmt.Sprintf("%d/0", num)
	}
	if num%den == 0 {
		return strconv.FormatInt(num/den, 10)
	}
	return strconv.FormatFloat(float64(num)/float64(den), 'g', -1, 64)
}
