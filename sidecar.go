package main

import (
	"fmt"
	"io"
)

// WriteSidecar writes tags as "Key: Value" lines, one tag per line, in
// the order they were read from the EXIF data.
func WriteSidecar(w io.Writer, tags []Tag) {
	for _, t := range tags {
		fmt.Fprintf(w, "%s: %s\n", t.Name, escapeValue(t.Value))
	}
}

// escapeValue collapses embedded newlines so a single tag can't be split
// across lines and misread as multiple tags when the sidecar is read back.
func escapeValue(v string) string {
	out := make([]rune, 0, len(v))
	for _, r := range v {
		if r == '\n' || r == '\r' {
			out = append(out, ' ')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
