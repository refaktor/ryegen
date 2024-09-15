package binder

import "github.com/refaktor/ryegen/ir"

// Dependencies tracks the dependencies used while generating code.
type Dependencies struct {
	Imports               map[string]struct{}
	Types                 map[string]ir.Ident
	GenericInterfaceImpls map[string]*ir.Interface
}

func NewDependencies() *Dependencies {
	return &Dependencies{
		Imports:               make(map[string]struct{}),
		Types:                 make(map[string]ir.Ident),
		GenericInterfaceImpls: make(map[string]*ir.Interface),
	}
}

func (deps *Dependencies) MarkUsed(id ir.Ident) {
	if id.File == nil {
		return
	}
	deps.Types[id.GoName] = id
	for _, imp := range id.UsedImports {
		deps.Imports[imp.ModulePath] = struct{}{}
	}
}
