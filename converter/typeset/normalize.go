package typeset

import (
	"fmt"
	"go/token"
	"go/types"
	"hash/fnv"
	"slices"

	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

func normalizeFlat(typ types.Type) types.Type {
	// Aliased types always behave exactly the same as the
	// type R in "type A = R", even if they're nested within
	// other types.
	typ = types.Unalias(typ)

	switch t := typ.(type) {
	case *types.Signature:
		if t.Recv() != nil {
			// Turn receiver into regular parameter for conversion.

			if t.TypeParams().Len() > 0 {
				// https://cs.opensource.google/go/go/+/master:src/go/types/signature.go;l=93;drc=b4309ece66ca989a38ed65404850a49ae8f92742
				panic("generic method cannot have any type params")
			}

			// Func signatures with a receiver can't have any
			// type params outside of their receiver, so transfer
			// receiver type params to new func body type params.
			var tParams []*types.TypeParam
			for tParam := range t.RecvTypeParams().TypeParams() {
				tParams = append(tParams, types.NewTypeParam(
					tParam.Obj(),
					tParam.Constraint(),
				))
			}

			typ = types.NewSignatureType(
				nil,
				nil,
				tParams,
				types.NewTuple(append(
					[]*types.Var{t.Recv()},
					slices.Collect(t.Params().Variables())...,
				)...),
				t.Results(),
				t.Variadic(),
			)
		}
	case *types.Interface:
		if t.NumMethods() == 0 {
			typ = types.Universe.Lookup("any").Type()
			return typ
		}
	}
	return typ
}

func typeHash(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return fmt.Sprintf("%016x", h.Sum64())
}

// Resolves aliases, moves signature receiver
// into params and creates aliases for any
// structs.
// Type strings and normalized types are recorded into
// the ts's cache and created aliases are recorded into ts's
// aliases.
func (ts *TypeSet) normalizeAndAddType(typ types.Type) types.Type {
	unnormalizedType := typ

	typ = normalizeFlat(typ)

	// Make sure the inner types are normalized and processed first.
	typ = walktypes.WalkModify(typ, ts.normalizeAndAddType)

	if struc, ok := typ.(*types.Struct); ok {
		name := "struct_" + typeHash(typ.String())
		typ = types.NewAlias(
			types.NewTypeName(token.NoPos, nil, name, nil),
			typ,
		)
		ts.aliases[name] = struc
	}
	ts.nameCache[unnormalizedType] = types.TypeString(typ, ts.qualifier)
	ts.normCache[unnormalizedType] = typ

	return typ
}
