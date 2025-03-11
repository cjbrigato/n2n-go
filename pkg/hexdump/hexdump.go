package hexdump

import (
	"fmt"
	"io"
	"unicode"
)

// FHexDump writes a formatted hex dump of data to the provided writer.
// displayAddr is the starting address offset to display in the output.
func FHexDump(displayAddr uint, data []byte, w io.Writer) error {
	const bytesPerLine = 16
	for i := 0; i < len(data); i += bytesPerLine {
		// Compute the current line offset.
		offset := displayAddr + uint(i)
		// Determine end index for this line.
		end := i + bytesPerLine
		if end > len(data) {
			end = len(data)
		}
		line := data[i:end]

		// Print offset.
		if _, err := fmt.Fprintf(w, "%08x  ", offset); err != nil {
			return err
		}

		// Print hex bytes.
		for j := 0; j < bytesPerLine; j++ {
			if j < len(line) {
				if _, err := fmt.Fprintf(w, "%02x ", line[j]); err != nil {
					return err
				}
			} else {
				// Pad if not enough bytes on the last line.
				if _, err := fmt.Fprint(w, "   "); err != nil {
					return err
				}
			}
			// Extra space after 8 bytes.
			if j == 7 {
				if _, err := fmt.Fprint(w, " "); err != nil {
					return err
				}
			}
		}

		// Print ASCII representation.
		if _, err := fmt.Fprint(w, " |"); err != nil {
			return err
		}
		for _, b := range line {
			if unicode.IsPrint(rune(b)) {
				if _, err := fmt.Fprintf(w, "%c", b); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprint(w, "."); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintln(w, "|"); err != nil {
			return err
		}
	}
	return nil
}
