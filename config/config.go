package config

import (
	"bytes"
	_ "embed"
	"errors"
	"os"
	"regexp"
	"strconv"

	"dario.cat/mergo"
	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Imports []string `toml:"imports"`
	Targets []struct {
		Name       string `toml:"name"`
		GOOS       string `toml:"goos"`
		GOARCH     string `toml:"goarch"`
		CGoEnabled bool   `toml:"cgo-enabled"`
		Tags       string `toml:"tags"`
	} `toml:"target"`
	Sources []struct {
		Packages []string `toml:"packages"`
	} `toml:"source"`
	Rules []struct {
		Select struct {
			Package *regexp.Regexp `toml:"package"`
			Name    *regexp.Regexp `toml:"name"`
			Type    string         `toml:"type"`
		} `toml:"select"`
		Actions struct {
			Rename   string `toml:"rename"`
			Include  bool   `toml:"include"`
			ToCasing string `toml:"to-casing"`
		} `toml:"action"`
	} `toml:"rule"`
	Converters []struct {
		Type      *regexp.Regexp `toml:"type"`
		Templates struct {
			ToRye   string `toml:"to-rye"`
			FromRye string `toml:"from-rye"`
		} `toml:"template"`
	} `toml:"converter"`
	ConverterHelpers []struct {
		Name      string `toml:"name"`
		Templates struct {
			ToRye   string `toml:"to-rye"`
			FromRye string `toml:"from-rye"`
		} `toml:"template"`
	} `toml:"converter-helper"`
}

type Error struct {
	filePath string
	err      error  // short, single-line error
	str      string // full, multi-line error string, or err string, if none
}

func (e *Error) Error() string {
	return e.filePath + ": " + e.err.Error()
}

func (e *Error) String() string {
	if e.str != "" {
		return "Error in file " + strconv.Quote(e.filePath) + ":\n" + e.str
	} else {
		return e.Error()
	}
}

func (e *Error) Unwrap() error {
	return e.err
}

func Load(path string) (_ *Config, err error) {
	defer func() {
		if err != nil {
			if tErr := (&toml.DecodeError{}); errors.As(err, &tErr) {
				err = &Error{filePath: path, err: err, str: tErr.String()}
			} else if tErr := (&toml.StrictMissingError{}); errors.As(err, &tErr) {
				err = &Error{filePath: path, err: err, str: tErr.String()}
			} else {
				err = &Error{filePath: path, err: err}
			}
		}
	}()

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	c := &Config{}
	err = toml.NewDecoder(bytes.NewReader(file)).
		DisallowUnknownFields().
		Decode(&c)
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

//go:embed default.toml
var DefaultConfig []byte
