package bindspec

import "regexp"

type SelectorType int

const (
	SelPkg SelectorType = iota
	SelName
)

type Action int

const (
	Rename Action = iota
	ToKebab
	Include
	Exclude
)

type Selector struct {
	Type SelectorType
	// Invert selector
	Not bool
	// Regex
	Regexp *regexp.Regexp
}

type Stmt struct {
	LineNo    int
	Selectors []Selector
	// What to do with selection.
	Action      Action
	ActionParam string
}

type Program struct {
	Filename string
	Body     []*Stmt
}
