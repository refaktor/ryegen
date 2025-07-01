package module

import "strings"

type Package struct {
	Path    string
	Version string
}

func NewPackage(path, version string) Package {
	return Package{Path: path, Version: version}
}

func (p Package) String() string {
	if p.Version == "" {
		return p.Path
	} else {
		return p.Path + "@" + p.Version
	}
}

func (p Package) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

func (p *Package) UnmarshalText(text []byte) error {
	p.Path, p.Version, _ = strings.Cut(string(text), "@")
	return nil
}
