package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"go/build/constraint"
	"os"
	"regexp"

	"dario.cat/mergo"
	"github.com/pelletier/go-toml/v2"
)

type Constraint struct {
	constraint.Expr
}

func (c *Constraint) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

func (c *Constraint) UnmarshalText(text []byte) error {
	expr, err := constraint.Parse("//go:build " + string(text))
	if err != nil {
		return err
	}
	c.Expr = expr
	return nil
}

type Target struct {
	Select     *Constraint `toml:"select"`
	CGoEnabled *bool       `toml:"cgo-enabled"`
}

type Source struct {
	Packages []string `toml:"packages"`
}

type Rule struct {
	Select struct {
		Package *regexp.Regexp `toml:"package"`
		Name    *regexp.Regexp `toml:"name"`
		Recv    *regexp.Regexp `toml:"recv"`
		Type    string         `toml:"type"`
		TypePos toml.FieldPosition
	} `toml:"select"`
	Actions struct {
		Include       *bool  `toml:"include"`
		Rename        string `toml:"rename"`
		RenamePos     toml.FieldPosition
		ToCasing      string `toml:"to-casing"`
		ToCasingPos   toml.FieldPosition
		SetPackage    string `toml:"set-package"`
		SetPackagePos toml.FieldPosition
	} `toml:"action"`
}

type Converter struct {
	Type      *regexp.Regexp `toml:"type"`
	Templates struct {
		ToRye   string `toml:"to-rye"`
		FromRye string `toml:"from-rye"`
	} `toml:"template"`
}

type ConverterHelper struct {
	Name      string `toml:"name"`
	Templates struct {
		ToRye   string `toml:"to-rye"`
		FromRye string `toml:"from-rye"`
	} `toml:"template"`
}

type Config struct {
	MakeError        toml.ErrorMaker   `toml:"-"`
	Imports          []string          `toml:"imports"`
	Targets          []Target          `toml:"target"`
	Sources          []Source          `toml:"source"`
	Rules            []Rule            `toml:"rule"`
	Converters       []Converter       `toml:"converter"`
	ConverterHelpers []ConverterHelper `toml:"converter-helper"`
}

type Error struct {
	filePath string
	err      error // short, single-line error
	row, col int
	str      string // full, multi-line error string, or err string, if none
}

// Error returns a short error message.
func (e *Error) Error() string {
	return e.filePath + ": " + e.err.Error()
}

// String returns the full multi-line error string.
func (e *Error) String() string {
	if e.str != "" {
		var loc string
		if e.row != 0 && e.col != 0 {
			loc = fmt.Sprintf(":%v:%v", e.row, e.col)
		}
		return fmt.Sprintf("Error in %v%v:\n%v", e.filePath, loc, e.str)
	} else {
		return e.Error()
	}
}

func (e *Error) Unwrap() error {
	return e.err
}

func wrapError(path string, err error) error {
	if tErr := (&toml.DecodeError{}); errors.As(err, &tErr) {
		row, col := tErr.Position()
		return &Error{filePath: path, err: err, row: row, col: col, str: tErr.String()}
	} else if tErr := (&toml.StrictMissingError{}); errors.As(err, &tErr) {
		return &Error{filePath: path, err: err, str: tErr.String()}
	} else {
		return &Error{filePath: path, err: err}
	}
}

func Load(path string) (_ *Config, err error) {
	defer func() {
		if err != nil {
			err = wrapError(path, err)
		}
	}()

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	c := &Config{}
	dec := toml.NewDecoder(bytes.NewReader(file))
	dec.DisallowUnknownFields()
	errorMaker := dec.ErrorMaker()
	c.MakeError = func(pos toml.FieldPosition, format string, args ...any) error {
		return wrapError(path, errorMaker(pos, format, args...).(*toml.DecodeError))
	}
	err = dec.Decode(&c)
	if err != nil {
		return nil, err
	}

	var importedCs []*Config // collect imported files first so their imports don't leak into our file's imports
	for _, imp := range c.Imports {
		newC, err := Load(imp)
		if err != nil {
			return nil, err
		}
		importedCs = append(importedCs, newC)
	}
	for _, newC := range importedCs {
		if err := mergo.Merge(c, newC, mergo.WithAppendSlice); err != nil {
			return nil, err
		}
	}

	return c, nil
}
