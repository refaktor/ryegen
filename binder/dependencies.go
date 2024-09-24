package binder

import "github.com/refaktor/ryegen/ir"

// Dependencies tracks the dependencies used while generating code.
type Dependencies struct {
	Imports               map[string]struct{}
	GenericInterfaceImpls map[string]*ir.Interface
}

func NewDependencies() *Dependencies {
	return &Dependencies{
		Imports:               make(map[string]struct{}),
		GenericInterfaceImpls: make(map[string]*ir.Interface),
	}
}

func (deps *Dependencies) MarkUsed(id ir.Ident) {
	if id.File == nil {
		return
	}
	for _, imp := range id.UsedImports {
		deps.Imports[imp.ModulePath] = struct{}{}
	}
}
