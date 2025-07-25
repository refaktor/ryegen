package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/refaktor/ryegen/v2/textutils"
)

type LogLevel int

const (
	INFO  LogLevel = 0
	WARN  LogLevel = 1
	ERROR LogLevel = 2
	FATAL LogLevel = 99
)

type Logger struct {
	Writer   io.Writer
	Prefix   string
	MinLevel LogLevel
}

func (l *Logger) Log(level LogLevel, format string, args ...any) {
	if l.Writer == nil || level < l.MinLevel {
		return
	}
	var b bytes.Buffer
	if l.Prefix != "" {
		b.WriteString(l.Prefix)
		b.WriteString(" ")
	}
	switch level {
	case INFO:
		b.WriteString("INFO")
	case WARN:
		b.WriteString("WARNING")
	case ERROR:
		b.WriteString("ERROR")
	case FATAL:
		b.WriteString("FATAL")
	default:
		panic(fmt.Sprintf("invalid log level: %v", level))
	}
	b.WriteString(":")
	s := fmt.Sprintf(format, args...)
	if strings.Contains(s, "\n") {
		b.WriteString("\n")
		s = textutils.IndentString(s, "  ", 1)
	} else {
		b.WriteString(" ")
	}
	b.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		b.WriteString("\n")
	}
	if _, err := io.Copy(l.Writer, &b); err != nil {
		// TODO: should we do something here?
	}
	if level == FATAL {
		os.Exit(1)
	}
}
