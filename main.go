// Command exifsidecar converts between EXIF metadata embedded in JPEG
// files and a plain text sidecar format that's easy to grep, diff, and
// edit by hand.
package main

import (
	"bufio"
	"bytes"
	"errors"
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
	case "embed":
		if err := runEmbed(os.Args[2:]); err != nil {
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
       exifsidecar embed <photo.jpg> <sidecar.txt>

extract reads a JPEG file (or stdin, if no file or "-" is given), pulls
the EXIF tags out of it, and writes them to stdout as a plain text
sidecar file in "Key: Value" form, one tag per line.

embed reads a JPEG file and a sidecar file, and writes a copy of the
JPEG with its EXIF data replaced by the sidecar's tags to stdout. One
of <photo.jpg> or <sidecar.txt> (but not both) may be "-" for stdin.

Examples:
  exifsidecar extract photo.jpg > photo.jpg.txt
  cat photo.jpg | exifsidecar extract - > photo.jpg.txt
  exifsidecar embed photo.jpg photo.jpg.txt > new.jpg`)
}

func runExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	fs.Parse(args)

	name := "-"
	if fs.NArg() > 0 {
		name = fs.Arg(0)
	}

	data, err := readFileOrStdin(name)
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

func runEmbed(args []string) error {
	fs := flag.NewFlagSet("embed", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() != 2 {
		return errors.New("usage: exifsidecar embed <photo.jpg> <sidecar.txt>")
	}
	jpegName, sidecarName := fs.Arg(0), fs.Arg(1)
	if jpegName == "-" && sidecarName == "-" {
		return errors.New(`only one of <photo.jpg> or <sidecar.txt> can be "-" (stdin)`)
	}

	jpegData, err := readFileOrStdin(jpegName)
	if err != nil {
		return fmt.Errorf("reading %s: %w", jpegName, err)
	}
	sidecarData, err := readFileOrStdin(sidecarName)
	if err != nil {
		return fmt.Errorf("reading %s: %w", sidecarName, err)
	}

	tags, err := ParseSidecar(bytes.NewReader(sidecarData))
	if err != nil {
		return fmt.Errorf("parsing sidecar: %w", err)
	}

	tiff, err := BuildEXIF(tags)
	if err != nil {
		return fmt.Errorf("building EXIF data: %w", err)
	}

	out, err := EmbedEXIF(jpegData, tiff)
	if err != nil {
		return fmt.Errorf("embedding EXIF data: %w", err)
	}

	_, err = os.Stdout.Write(out)
	return err
}

func readFileOrStdin(name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(bufio.NewReader(os.Stdin))
	}
	return os.ReadFile(name)
}
