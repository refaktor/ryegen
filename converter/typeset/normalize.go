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
			// Move receiver type params into function's type params. Avoid duplicating
			// bounds or parameters by reusing the existing params rather than cloning.
			recvTParams := slices.Collect(t.RecvTypeParams().TypeParams())
			// Deduplicate by identity just in case.
			if len(recvTParams) > 1 {
				seen := make(map[*types.TypeParam]struct{}, len(recvTParams))
				filtered := make([]*types.TypeParam, 0, len(recvTParams))
				for _, p := range recvTParams {
					if _, ok := seen[p]; ok {
						continue
					}
					seen[p] = struct{}{}
					filtered = append(filtered, p)
				}
				recvTParams = filtered
			}

			// Build new signature; if go/types panics due to bounds, drop generics.
			var newSig *types.Signature
			func() {
				defer func() {
					if r := recover(); r != nil {
						newSig = types.NewSignatureType(
							nil,
							nil,
							nil,
							types.NewTuple(append(
								[]*types.Var{t.Recv()},
								slices.Collect(t.Params().Variables())...,
							)...),
							t.Results(),
							t.Variadic(),
						)
					}
				}()
				newSig = types.NewSignatureType(
					nil,
					nil,
					recvTParams,
					types.NewTuple(append(
						[]*types.Var{t.Recv()},
						slices.Collect(t.Params().Variables())...,
					)...),
					t.Results(),
					t.Variadic(),
				)
			}()
			if newSig == nil {
				newSig = types.NewSignatureType(nil, nil, nil,
					types.NewTuple(append(
						[]*types.Var{t.Recv()},
						slices.Collect(t.Params().Variables())...,
					)...),
					t.Results(),
					t.Variadic(),
				)
			}
			typ = newSig
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
	if typ == nil {
		// Bail out early if normalization produced an invalid type
		ts.nameCache[unnormalizedType] = "<invalid>"
		ts.normCache[unnormalizedType] = nil
		return nil
	}

	if struc, ok := typ.(*types.Struct); ok {
		name := "struct_" + typeHash(typ.String())
		typ = types.NewAlias(
			types.NewTypeName(token.NoPos, nil, name, nil),
			typ,
		)
		ts.aliases[name] = struc
	}
	// Guard against nil types in case upstream normalization dropped a problematic signature.
	if typ == nil {
		// Record as empty to avoid crashing; caller should handle missing type.
		ts.nameCache[unnormalizedType] = "<invalid>"
		return typ
	}
	ts.nameCache[unnormalizedType] = types.TypeString(typ, ts.qualifier)
	ts.normCache[unnormalizedType] = typ

	return typ
}
