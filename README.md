# exif-sidecar

JPEG files carry metadata (camera make and model, exposure settings,
capture time) as binary EXIF data packed into an APP1 segment. That's
fine for cameras and photo tools, but it's opaque to `grep`, `diff`,
version control, or a text editor. This tool pulls the EXIF tags out of
a JPEG and writes them as a plain text sidecar file, one `Key: Value`
pair per line.

## Usage

Read from a file:

```
exifsidecar extract photo.jpg > photo.jpg.txt
```

Read from stdin, either explicitly or by omitting the file argument:

```
cat photo.jpg | exifsidecar extract - > photo.jpg.txt
exifsidecar extract < photo.jpg > photo.jpg.txt
```

Write a sidecar's tags back into a JPEG's EXIF data:

```
exifsidecar embed photo.jpg photo.jpg.txt > new.jpg
```

One of the two arguments (but not both) can be `-` to read that input
from stdin. `embed` replaces whatever APP1 Exif segment the JPEG
already has, or inserts a new one right after the SOI marker if it
doesn't have one.

Example output:

```
Make: Canon
Model: Canon EOS 5D Mark IV
Orientation: 1
DateTime: 2024:03:12 14:07:31
ExposureTime: 1/500
FNumber: 4
ISOSpeedRatings: 200
FocalLength: 85
```

Unrecognized tags are still included, labeled by their numeric ID
(`Tag0x9286` for an IFD0 tag, `ExifTag0x927C` for one in the Exif SubIFD,
`GPSTag0x001F` for one in the GPSInfo IFD, `ThumbnailTag0x010F` for one
in the IFD1 thumbnail directory), so nothing silently disappears, and
`embed` puts each one back in the IFD it came from.

A JPEG-format thumbnail (the kind cameras and phones actually embed)
comes out as a single `ThumbnailImage` tag holding the thumbnail's own
JPEG bytes as hex, alongside a few small `Thumbnail*` metadata tags
(compression, resolution). `embed` reproduces both from those tags.

## Building

Standard library only, no external modules:

```
go build .
```

## Status

Both directions are implemented for IFD0, the Exif SubIFD, the GPSInfo
IFD, and IFD1 (the thumbnail directory, for the common JPEG-thumbnail
case). Tags neither side recognizes by name, including MakerNote,
round-trip as opaque bytes in whichever IFD they were found in. There's
no automated test coverage yet.

## License

MIT, see LICENSE.
