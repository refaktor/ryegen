package textutils

import (
	"bytes"
	"slices"
	"strings"
)

var asciiSpace = [256]bool{'\t': true, '\n': true, '\v': true, '\f': true, '\r': true, ' ': true}

// Prepends indent nIndent times to each line beginning in s,
// except for empty lines.
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
		hitNewline := false
		end = bytes.Index(b[start:], []byte{'\n'})
		if end == -1 {
			end = len(b)
		} else {
			hitNewline = true
			end += start + 1 // adjust to offset and include "\n"
		}
		for range nIndent {
			res.WriteString(indent)
		}
		line := b[start:end]
		if slices.ContainsFunc(line, func(b byte) bool { return !asciiSpace[b] }) {
			res.Write(line)
		} else if hitNewline {
			res.Write([]byte{'\n'})
		}
		start = end
	}

	return res.String()
}
