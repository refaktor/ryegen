package binder

import (
	"github.com/refaktor/ryegen/config"
	"github.com/refaktor/ryegen/ir"
)

// Immutable
type Context struct {
	Config   *config.Config
	IR       *ir.IR
	ModNames ir.UniqueModuleNames
}

func NewContext(cfg *config.Config, irData *ir.IR, modNames ir.UniqueModuleNames) *Context {
	return &Context{
		Config:   cfg,
		IR:       irData,
		ModNames: modNames,
	}
}
