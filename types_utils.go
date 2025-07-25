package main

import (
	"go/constant"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/refaktor/ryegen/v2/converter/typeset"
	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

func collectImports(t types.Type) []*types.Package {
	imports := map[string]*types.Package{}
	var doCollectImports func(t types.Type)
	doCollectImports = func(t types.Type) {
		switch t := t.(type) {
		case *types.Named:
			if t.Obj().Exported() {
				if pkg := t.Obj().Pkg(); pkg != nil {
					imports[pkg.Path()] = pkg
				}
			}
		case *types.Basic:
			if t.Kind() == types.UnsafePointer {
				imports["unsafe"] = types.Unsafe
			}
		}
		walktypes.Walk(t, doCollectImports)
	}
	doCollectImports(t)
	return slices.Collect(maps.Values(imports))
}

func addStructAliasTypes(structAliases map[string]*types.Alias, tset *typeset.TypeSet, t types.Type) {
	var doAddStructAliasTypes func(t types.Type)
	doAddStructAliasTypes = func(t types.Type) {
		if tset.ContainsAlias(t) {
			alias := t.(*types.Alias)
			structAliases[alias.Obj().Name()] = alias
		}
		walktypes.Walk(t, doAddStructAliasTypes)
	}
	doAddStructAliasTypes(t)
}

// Returns the type of t after removing
// all indirections.
func receiverTypeNameNoPtr(t types.Type) string {
	for {
		if pt, ok := t.(*types.Pointer); ok {
			t = pt.Elem()
		} else {
			break
		}
	}
	return t.String()
}

// Returns pkg+"."+name (or just name if no pkg).
func objectString(obj types.Object, qf types.Qualifier) string {
	var b strings.Builder
	if obj.Pkg() != nil {
		pkg := qf(obj.Pkg())
		if pkg != "" {
			b.WriteString(pkg)
			b.WriteString(".")
		}
	}
	b.WriteString(obj.Name())
	return b.String()
}

// resolveUntyped returns a proper type that can be assigned
// to from obj's untyped type.
// If obj is already properly typed or not a *types.Const, returns
// obj.Type().
func resolveUntyped(obj types.Object) types.Type {
	typ := obj.Type()
	bt, ok := typ.(*types.Basic)
	if !ok {
		return typ
	}

	if bt.Info()&types.IsUntyped == 0 {
		return typ
	}

	cnst := obj.(*types.Const) // only const can be untyped

	switch {
	case bt.Kind() == types.UntypedBool:
		return types.Typ[types.Bool]
	case bt.Kind() == types.UntypedInt:
		if _, ok := constant.Int64Val(cnst.Val()); !ok {
			return types.Typ[types.Uint64]
		}
		return types.Typ[types.Int64]
	case bt.Kind() == types.UntypedRune:
		return types.Typ[types.Rune]
	case bt.Kind() == types.UntypedFloat:
		return types.Typ[types.Float64]
	case bt.Kind() == types.UntypedComplex:
		return types.Typ[types.Complex128]
	case bt.Kind() == types.UntypedString:
		return types.Typ[types.String]
	case bt.Kind() == types.UntypedNil:
		return types.NewPointer(types.NewStruct(nil, nil))
	default:
		panic("unknown untyped type: " + bt.Name())
	}
}
