package module

import (
	"strings"

	"golang.org/x/mod/module"
)

// Basically like golang.org/x/mod/module, but implementing TextMarshaler
// and with some utility methods.
type Module struct {
	Path    string
	Version string
}

func New(path, version string) Module {
	return Module{
		Path:    path,
		Version: version,
	}
}

// Unlike x/mod/module.Check, this won't error
// on a path where the first element is missing
// a dot.
func (m *Module) Check() error {
	err := module.Check(m.Path, m.Version)
	if _, ok := err.(*module.InvalidPathError); ok {
		// This will report a std library path as invalid, which it isn't.
		return nil
	}
	return err
}

func (m Module) Escape() (Module, error) {
	p, err := EscapePath(m.Path)
	if err != nil {
		return Module{}, err
	}
	v, err := EscapeVersion(m.Version)
	if err != nil {
		return Module{}, err
	}
	return New(p, v), nil
}

func (m Module) Unscape() (Module, error) {
	p, err := UnescapePath(m.Path)
	if err != nil {
		return Module{}, err
	}
	v, err := UnescapeVersion(m.Version)
	if err != nil {
		return Module{}, err
	}
	return New(p, v), nil
}

func (m Module) String() string {
	if m.Version == "" {
		return m.Path
	} else {
		return m.Path + "@" + m.Version
	}
}

func (m Module) MarshalText() ([]byte, error) {
	if err := m.Check(); err != nil {
		return nil, err
	}
	return []byte(m.String()), nil
}

func (m *Module) UnmarshalText(text []byte) error {
	m.Path, m.Version, _ = strings.Cut(string(text), "@")
	if err := m.Check(); err != nil {
		return err
	}
	return nil
}

// Returns true if s is a package or module path in
// the std library, i.e. the first element contains
// no dot.
// Doesn't actually check if the std library package
// exists.
// Returns false if s is empty.
func IsPkgPathStd(s string) bool {
	firstElem, _, _ := strings.Cut(s, "/")
	return !strings.Contains(firstElem, ".")
}

func EscapePath(s string) (string, error)      { return module.EscapePath(s) }
func EscapeVersion(s string) (string, error)   { return module.EscapeVersion(s) }
func UnescapePath(s string) (string, error)    { return module.UnescapePath(s) }
func UnescapeVersion(s string) (string, error) { return module.UnescapeVersion(s) }
