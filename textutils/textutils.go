package textutils

import (
	"bytes"
	"slices"
	"strings"
)

var asciiSpace = [256]bool{'\t': true, '\n': true, '\v': true, '\f': true, '\r': true, ' ': true}

// Prepends indent nIndent times to each line beginning in s.
// Leaves lines that are empty or contain only spaces empty.
func IndentString(s string, indent string, nIndent int) string {
	b := []byte(s)

	var res strings.Builder
	{
		nBOL := bytes.Count(b, []byte{'\n'}) + 1
		upperBound := len(s) + nBOL*nIndent*len(indent) // doesn't consider the fact that empty lines are ignored
		res.Grow(upperBound)
	}

	start := 0
	end := 0
	for start < len(b) {
		crlf := false
		hitNewline := false
		end = bytes.Index(b[start:], []byte{'\n'})
		if end == -1 {
			end = len(b)
		} else {
			hitNewline = true
			end += start + 1 // adjust to offset and include "\n"
			crlf = end-2 < len(b) && b[end-2] == '\r'
		}
		line := b[start:end]
		if slices.ContainsFunc(line, func(b byte) bool { return !asciiSpace[b] }) {
			for range nIndent {
				res.WriteString(indent)
			}
			res.Write(line)
		} else if hitNewline {
			if crlf {
				res.WriteByte('\r')
			}
			res.WriteByte('\n')
		}
		start = end
	}

	return res.String()
}
