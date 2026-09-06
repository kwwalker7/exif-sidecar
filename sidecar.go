package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// WriteSidecar writes tags as "Key: Value" lines, one tag per line, in
// the order they were read from the EXIF data.
func WriteSidecar(w io.Writer, tags []Tag) {
	for _, t := range tags {
		fmt.Fprintf(w, "%s: %s\n", t.Name, escapeValue(t.Value))
	}
}

// ParseSidecar reads "Key: Value" lines back into tags, in the order
// they appear. Blank lines are ignored so a sidecar can be edited by
// hand without worrying about trailing whitespace at the end of the
// file.
func ParseSidecar(r io.Reader) ([]Tag, error) {
	var tags []Tag
	sc := bufio.NewScanner(r)
	// Some tag values (thumbnail bytes as hex, for instance) can run
	// much longer than bufio.Scanner's 64KB default line limit.
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		idx := strings.Index(line, ": ")
		if idx < 0 {
			return nil, fmt.Errorf("line %d: expected \"Key: Value\", got %q", lineNo, line)
		}
		tags = append(tags, Tag{Name: line[:idx], Value: line[idx+2:]})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no tags found in sidecar")
	}
	return tags, nil
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
