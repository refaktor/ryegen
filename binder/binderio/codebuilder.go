package binderio

import (
	"bufio"
	"fmt"
	"go/format"
	"os"
	"strings"
)

type CodeBuilder struct {
	b strings.Builder

	Indent int
}

func (w *CodeBuilder) Write(s string) {
	w.b.WriteString(s)
}

// Appends with correct indentation
func (w *CodeBuilder) Append(s string) {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		w.Linef("%v", sc.Text())
	}
}

func (w *CodeBuilder) Linef(format string, args ...any) {
	for i := 0; i < w.Indent; i++ {
		w.b.WriteString("\t")
	}
	w.b.WriteString(fmt.Sprintf(format, args...))
	w.b.WriteString("\n")
}

func (w *CodeBuilder) String() string {
	return w.b.String()
}

func (w *CodeBuilder) FmtString() (string, error) {
	code := []byte(w.String())
	code, err := format.Source(code)
	if err != nil {
		return "", err
	}
	return string(code), nil
}

func (w *CodeBuilder) Reset() {
	w.b.Reset()
}

func (w *CodeBuilder) SaveToFile(outFile string) (fmtErr error, err error) {
	code, err := w.FmtString()
	if err != nil {
		fmtErr = err
		code = w.String()
	}
	if err := os.WriteFile(outFile, []byte(code), 0666); err != nil {
		return fmtErr, err
	}
	return fmtErr, nil
}
