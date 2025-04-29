package textutils

import (
	"bytes"
	"strings"
)

// Prepends indent nIndent times to each line beginning in s.
func IndentString(s string, indent string, nIndent int) string {
	b := []byte(s)

	var res strings.Builder
	{
		nBOL := bytes.Count(b, []byte{'\n'}) + 1
		res.Grow(len(s) + nBOL*nIndent*len(indent))
	}

	start := 0
	end := 0
	for start < len(b) {
		end = bytes.Index(b[start:], []byte("\n"))
		if end == -1 {
			end = len(b)
		} else {
			end += start + 1 // adjust to offset and include "\n"
		}
		for range nIndent {
			res.WriteString(indent)
		}
		res.Write(b[start:end])
		start = end
	}

	return res.String()
}
