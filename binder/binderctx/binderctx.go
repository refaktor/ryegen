package binderctx

import (
	"github.com/refaktor/ryegen/config"
	"github.com/refaktor/ryegen/ir"
)

type Context struct {
	Config      *config.Config
	IR          *ir.IR
	ModNames    ir.UniqueModuleNames
	UsedImports map[string]struct{}
	UsedTyps    map[string]ir.Ident
}

func New(cfg *config.Config, irData *ir.IR, modNames ir.UniqueModuleNames) *Context {
	return &Context{
		Config:      cfg,
		IR:          irData,
		ModNames:    modNames,
		UsedImports: make(map[string]struct{}),
		UsedTyps:    make(map[string]ir.Ident),
	}
}

func (c *Context) MarkUsed(id ir.Ident) {
	if id.File == nil {
		return
	}
	c.UsedTyps[id.GoName] = id
	for _, imp := range id.UsedImports {
		c.UsedImports[imp.ModulePath] = struct{}{}
	}
}
