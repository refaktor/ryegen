package bindspec

import "regexp"

type Action int

const (
	Rename Action = iota
	ToKebab
	Exclude
)

type Stmt struct {
	// Invert package selector.
	NotPkg bool
	// Package selector, or nil to select all.
	PkgSelector *regexp.Regexp
	// Invert name selector.
	NotName bool
	// Name selector, or nil to select all.
	NameSelector *regexp.Regexp
	// What to do with selection.
	Action      Action
	ActionParam string
}
