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
(`Tag0x9286`, for example), so nothing silently disappears.

## Building

Standard library only, no external modules:

```
go build .
```

## Status

Only the JPEG-to-sidecar direction is implemented so far. Writing a
sidecar file's values back into a JPEG's EXIF data (the other half of
"converter") is still on the list. See the code for the current tag
coverage — IFD0 and the Exif SubIFD are handled; GPS tags and
thumbnail (IFD1) data are not yet.

## License

MIT, see LICENSE.
