package binderio

import (
	"bufio"
	"fmt"
	"go/format"
	"os"
	"strings"
)

// CodeBuilder is a wrapper around [strings.Builder] that simplifies
// building Go code.
//
// The zero value is safely ready to use.
type CodeBuilder struct {
	// Indent is the indentation level (indentation is tabs).
	Indent int

	b strings.Builder
}

// Write appends a raw string to the internal [strings.Builder].
func (w *CodeBuilder) Write(s string) {
	w.b.WriteString(s)
}

// Append writes the given string line by line with correct indentation.
func (w *CodeBuilder) Append(s string) {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		w.Linef("%v", sc.Text())
	}
}

// Linef writes a single line, prepended by the current indentation.
//
// Takes format and args like [fmt.Printf].
func (w *CodeBuilder) Linef(format string, args ...any) {
	for i := 0; i < w.Indent; i++ {
		w.b.WriteString("\t")
	}
	w.b.WriteString(fmt.Sprintf(format, args...))
	w.b.WriteString("\n")
}

// String returns the current code without applying any formatting.
func (w *CodeBuilder) String() string {
	return w.b.String()
}

// FmtString attempts to format the current code as Go source code.
func (w *CodeBuilder) FmtString() (string, error) {
	code := []byte(w.String())
	code, err := format.Source(code)
	if err != nil {
		return "", err
	}
	return string(code), nil
}

func (w *CodeBuilder) Reset() {
	w.Indent = 0
	w.b.Reset()
}

// SaveToFile attempts to format the current code as Go source code and
// write it to outFile.
//
// If a formatting error occurs, it is returned in fmtErr and the function
// attempts to write the unformatted code instead. If a file IO error
// occurs, it is returned in err.
func (w *CodeBuilder) SaveToFile(outFile string) (fmtErr error, err error) {
	code, err := w.FmtString()
	if err != nil {
		fmtErr = err
		code = w.String()
	}
	if err := os.WriteFile(outFile, []byte(code), 0666); err != nil {
		return nil, err
	}
	return fmtErr, nil
}
