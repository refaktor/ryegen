/*
Package walktypes simplifies recursively iterating over types from the Go [types] package.
*/
package walktypes

import (
	"fmt"
	"go/types"
	"slices"
)

// Walk calls fn on all immediate children of type t.
// Returns early if fn returns an error.
func Walk(t types.Type, fn func(types.Type) error) error {
	walk := func(types.Type) error {
		return fn(t)
	}
	walkVar := func(v *types.Var) error {
		return walk(v.Type())
	}
	walkTuple := func(t *types.Tuple) error {
		for v := range t.Variables() {
			if err := walkVar(v); err != nil {
				return err
			}
		}
		return nil
	}
	walkSignature := func(t *types.Signature) error {
		if t.Recv() != nil {
			if err := walkVar(t.Recv()); err != nil {
				return err
			}
		}
		if err := walkTuple(t.Params()); err != nil {
			return err
		}
		if err := walkTuple(t.Results()); err != nil {
			return err
		}
		return nil
	}

	switch t := t.(type) {
	case *types.Basic:
		return nil
	case *types.Alias:
		return walk(t.Rhs())
	case *types.Array:
		return walk(t.Elem())
	case *types.Slice:
		return walk(t.Elem())
	case *types.Struct:
		for f := range t.Fields() {
			if err := walkVar(f); err != nil {
				return err
			}
		}
		return nil
	case *types.Pointer:
		return walk(t.Elem())
	case *types.Tuple:
		return walkTuple(t)
	case *types.Signature:
		return walkSignature(t)
	case *types.Union:
		for term := range t.Terms() {
			if err := walk(term.Type()); err != nil {
				return err
			}
		}
		return nil
	case *types.Interface:
		for m := range t.ExplicitMethods() {
			if err := walkSignature(m.Signature()); err != nil {
				return err
			}
		}
		for e := range t.EmbeddedTypes() {
			if err := walk(e); err != nil {
				return err
			}
		}
		return nil
	case *types.Map:
		if err := walk(t.Key()); err != nil {
			return err
		}
		if err := walk(t.Elem()); err != nil {
			return err
		}
		return nil
	case *types.Chan:
		return walk(t.Elem())
	case *types.Named:
		return nil
	case *types.TypeParam:
		return nil
	case nil:
		return nil
	default:
		panic(fmt.Sprintf("Walk: unknown type %T", t))
	}
}

// WalkModify calls fn on all immediate children of type t,
// reconstructing each child with the new returned type.
// Only reconstructs if a call to fn actually returned
// a modified type. May still allocate some memory, though.
// Returns the modified type.
// Returns early if fn returns an error.
func WalkModify(t types.Type, fn func(types.Type) (types.Type, error)) (types.Type, error) {
	walk := func(t types.Type) (_ types.Type, changed bool, err error) {
		t1, err := fn(t)
		if err != nil {
			return nil, false, err
		}
		return t1, t1 != t, nil
	}
	walkVar := func(v *types.Var) (_ *types.Var, changed bool, err error) {
		t1, changed, err := walk(v.Type())
		if err != nil {
			return nil, false, err
		}
		if !changed {
			return v, false, nil
		}
		return types.NewVar(v.Pos(), v.Pkg(), v.Name(), t1), true, nil
	}
	walkTuple := func(t *types.Tuple) (_ *types.Tuple, changed bool, err error) {
		changed = false
		vars := make([]*types.Var, t.Len())
		for i := range t.Len() {
			v1, chngd, err := walkVar(t.At(i))
			if err != nil {
				return nil, false, err
			}
			if chngd {
				changed = true
			}
			vars[i] = v1
		}
		if !changed {
			return t, false, nil
		}
		return types.NewTuple(vars...), true, nil
	}
	walkSignature := func(t *types.Signature) (_ *types.Signature, changed bool, err error) {
		var recv1 *types.Var
		var recvChanged bool
		if t.Recv() != nil {
			var err error
			recv1, recvChanged, err = walkVar(t.Recv())
			if err != nil {
				return nil, false, err
			}
		}
		params1, paramsChanged, err := walkTuple(t.Params())
		if err != nil {
			return nil, false, err
		}
		results1, resultsChanged, err := walkTuple(t.Results())
		if err != nil {
			return nil, false, err
		}
		if !recvChanged && !paramsChanged && !resultsChanged {
			return t, false, err
		}
		return types.NewSignatureType(
			recv1,
			slices.Collect(t.RecvTypeParams().TypeParams()),
			slices.Collect(t.TypeParams().TypeParams()),
			params1,
			results1,
			t.Variadic(),
		), true, nil
	}

	switch t := t.(type) {
	case *types.Basic:
		return t, nil
	case *types.Alias:
		t1, changed, err := walk(t.Rhs())
		if err != nil {
			return nil, err
		}
		if !changed {
			return t, nil
		}
		return types.NewAlias(t.Obj(), t1), nil
	case *types.Array:
		t1, changed, err := walk(t.Elem())
		if err != nil {
			return nil, err
		}
		if !changed {
			return t, nil
		}
		return types.NewArray(t1, t.Len()), nil
	case *types.Slice:
		t1, changed, err := walk(t.Elem())
		if err != nil {
			return nil, err
		}
		if !changed {
			return t, nil
		}
		return types.NewSlice(t1), nil
	case *types.Struct:
		changed := false
		fields := make([]*types.Var, t.NumFields())
		for i := range t.NumFields() {
			v1, chngd, err := walkVar(t.Field(i))
			if err != nil {
				return nil, err
			}
			if chngd {
				changed = true
			}
			fields[i] = v1
		}
		if !changed {
			return t, nil
		}
		tags := make([]string, t.NumFields())
		for i := range t.NumFields() {
			tags[i] = t.Tag(i)
		}
		return types.NewStruct(fields, tags), nil
	case *types.Pointer:
		t1, changed, err := walk(t.Elem())
		if err != nil {
			return nil, err
		}
		if !changed {
			return t, nil
		}
		return types.NewPointer(t1), nil
	case *types.Tuple:
		t1, _, err := walkTuple(t)
		return t1, err
	case *types.Signature:
		t1, _, err := walkSignature(t)
		return t1, err
	case *types.Union:
		changed := false
		terms := make([]*types.Term, t.Len())
		for i := range t.Len() {
			term := t.Term(i)
			t1, chngd, err := walk(term.Type())
			if err != nil {
				return nil, err
			}
			if chngd {
				changed = true
			}
			terms[i] = types.NewTerm(term.Tilde(), t1)
		}
		if !changed {
			return t, nil
		}
		return types.NewUnion(terms), nil
	case *types.Interface:
		methods := make([]*types.Func, t.NumExplicitMethods())
		methodsChanged := false
		for i := range t.NumExplicitMethods() {
			f := t.ExplicitMethod(i)
			sig1, changed, err := walkSignature(f.Signature())
			if err != nil {
				return nil, err
			}
			if changed {
				methodsChanged = true
			}
			methods[i] = types.NewFunc(f.Pos(), f.Pkg(), f.Name(), sig1)
		}
		embeddeds := make([]types.Type, t.NumEmbeddeds())
		embeddedsChanged := false
		for i := range t.NumEmbeddeds() {
			e := t.EmbeddedType(i)
			e1, changed, err := walk(e)
			if err != nil {
				return nil, err
			}
			if changed {
				embeddedsChanged = true
			}
			embeddeds[i] = e1
		}
		if !methodsChanged && !embeddedsChanged {
			return t, nil
		}
		return types.NewInterfaceType(methods, embeddeds), nil
	case *types.Map:
		k1, kChanged, err := walk(t.Key())
		if err != nil {
			return nil, err
		}
		v1, vChanged, err := walk(t.Elem())
		if err != nil {
			return nil, err
		}
		if !kChanged && !vChanged {
			return t, nil
		}
		return types.NewMap(k1, v1), nil
	case *types.Chan:
		t1, changed, err := walk(t.Elem())
		if err != nil {
			return nil, err
		}
		if !changed {
			return t, nil
		}
		return types.NewChan(t.Dir(), t1), nil
	case *types.Named:
		return t, nil
	case *types.TypeParam:
		return t, nil
	case nil:
		return nil, nil
	default:
		panic(fmt.Sprintf("WalkModify: unknown type %T", t))
	}
}
