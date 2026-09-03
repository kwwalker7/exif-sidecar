// Command exifsidecar converts between EXIF metadata embedded in JPEG
// files and a plain text sidecar format that's easy to grep, diff, and
// edit by hand.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "extract":
		if err := runExtract(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "exifsidecar:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "exifsidecar: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: exifsidecar extract [file]

Reads a JPEG file (or stdin, if no file or "-" is given), pulls the
EXIF tags out of it, and writes them to stdout as a plain text sidecar
file in "Key: Value" form, one tag per line.

Examples:
  exifsidecar extract photo.jpg > photo.jpg.txt
  cat photo.jpg | exifsidecar extract - > photo.jpg.txt`)
}

func runExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	fs.Parse(args)

	name := "-"
	if fs.NArg() > 0 {
		name = fs.Arg(0)
	}

	var r io.Reader
	if name == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}

	data, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return fmt.Errorf("reading input: %w", err)
	}

	tags, err := ExtractEXIF(data)
	if err != nil {
		return fmt.Errorf("parsing EXIF: %w", err)
	}

	WriteSidecar(os.Stdout, tags)
	return nil
}
