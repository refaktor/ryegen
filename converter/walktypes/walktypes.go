/*
Package walktypes simplifies recursively iterating over types from the Go [types] package.

The functions [Walk], [WalkErr], [WalkModify] and [WalkModifyErr] will recursively iterate through all immediate children,
meaning all sub-types that are represented in the string representation of the parent type.
Therefore, named/aliased types' children won't be resolved.
*/
package walktypes

import (
	"fmt"
	"go/types"
	"slices"
)

// WalkErr calls fn on all immediate children of type t.
// Returns early if fn returns an error. See [Walk].
func WalkErr(t types.Type, fn func(types.Type) error) error {
	walk := func(t types.Type) error {
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
	walkSignature := func(t *types.Signature, ignoreRecv bool) error {
		if !ignoreRecv && t.Recv() != nil {
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
		return walkSignature(t, false)
	case *types.Union:
		for term := range t.Terms() {
			if err := walk(term.Type()); err != nil {
				return err
			}
		}
		return nil
	case *types.Interface:
		for m := range t.ExplicitMethods() {
			if err := walkSignature(m.Signature(), true); err != nil {
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

// Walk is exactly like [WalkErr], but without the ability
// to return an error.
func Walk(t types.Type, fn func(t types.Type)) {
	WalkErr(t, func(t types.Type) error {
		fn(t)
		return nil
	})
}

// WalkModifyErr calls fn on all immediate children of type t,
// reconstructing each child with the new returned type.
// Only reconstructs if a call to fn actually returned
// a modified type. May still allocate some memory, though.
// Returns the modified type.
// Returns early if fn returns an error.
// See [WalkModify].
func WalkModifyErr(t types.Type, fn func(types.Type) (types.Type, error)) (types.Type, error) {
	walk := func(t types.Type) (types.Type, error) {
		return fn(t)
	}
	walkVar := func(v *types.Var) (*types.Var, error) {
		t := v.Type()
		t1, err := walk(t)
		if err != nil {
			return nil, err
		}
		if t1 == t {
			return v, nil
		}
		return types.NewVar(v.Pos(), v.Pkg(), v.Name(), t1), nil
	}
	walkTuple := func(t *types.Tuple) (*types.Tuple, error) {
		changed := false
		vars := make([]*types.Var, t.Len())
		for i := range t.Len() {
			v := t.At(i)
			v1, err := walkVar(v)
			if err != nil {
				return nil, err
			}
			if v1 != v {
				changed = true
			}
			vars[i] = v1
		}
		if !changed {
			return t, nil
		}
		return types.NewTuple(vars...), nil
	}
	walkSignature := func(t *types.Signature, ignoreRecv bool) (*types.Signature, error) {
		recv := t.Recv()
		var recv1 *types.Var
		if !ignoreRecv && recv != nil {
			var err error
			recv1, err = walkVar(recv)
			if err != nil {
				return nil, err
			}
		}
		params := t.Params()
		params1, err := walkTuple(params)
		if err != nil {
			return nil, err
		}
		results := t.Results()
		results1, err := walkTuple(results)
		if err != nil {
			return nil, err
		}
		if recv1 == recv && params1 == params && results1 == results {
			return t, err
		}
		return types.NewSignatureType(
			recv1,
			slices.Collect(t.RecvTypeParams().TypeParams()),
			slices.Collect(t.TypeParams().TypeParams()),
			params1,
			results1,
			t.Variadic(),
		), nil
	}

	switch t := t.(type) {
	case *types.Basic:
		return t, nil
	case *types.Alias:
		rhs := t.Rhs()
		rhs1, err := walk(rhs)
		if err != nil {
			return nil, err
		}
		if rhs1 == rhs {
			return t, nil
		}
		return types.NewAlias(t.Obj(), rhs1), nil
	case *types.Array:
		elem := t.Elem()
		elem1, err := walk(elem)
		if err != nil {
			return nil, err
		}
		if elem1 == elem {
			return t, nil
		}
		return types.NewArray(elem1, t.Len()), nil
	case *types.Slice:
		elem := t.Elem()
		elem1, err := walk(elem)
		if err != nil {
			return nil, err
		}
		if elem1 == elem {
			return t, nil
		}
		return types.NewSlice(elem1), nil
	case *types.Struct:
		changed := false
		fields := make([]*types.Var, t.NumFields())
		for i := range t.NumFields() {
			f := t.Field(i)
			f1, err := walkVar(f)
			if err != nil {
				return nil, err
			}
			if f1 != f {
				changed = true
			}
			fields[i] = f1
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
		elem := t.Elem()
		elem1, err := walk(elem)
		if err != nil {
			return nil, err
		}
		if elem1 == elem {
			return t, nil
		}
		return types.NewPointer(elem1), nil
	case *types.Tuple:
		return walkTuple(t)
	case *types.Signature:
		return walkSignature(t, false)
	case *types.Union:
		changed := false
		terms := make([]*types.Term, t.Len())
		for i := range t.Len() {
			term := t.Term(i)
			tt := term.Type()
			tt1, err := walk(tt)
			if err != nil {
				return nil, err
			}
			if tt1 != tt {
				changed = true
			}
			terms[i] = types.NewTerm(term.Tilde(), tt1)
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
			sig := f.Signature()
			sig1, err := walkSignature(sig, true)
			if err != nil {
				return nil, err
			}
			if sig1 != sig {
				methodsChanged = true
			}
			methods[i] = types.NewFunc(f.Pos(), f.Pkg(), f.Name(), sig1)
		}
		embeddeds := make([]types.Type, t.NumEmbeddeds())
		embeddedsChanged := false
		for i := range t.NumEmbeddeds() {
			e := t.EmbeddedType(i)
			e1, err := walk(e)
			if err != nil {
				return nil, err
			}
			if e1 != e {
				embeddedsChanged = true
			}
			embeddeds[i] = e1
		}
		if !methodsChanged && !embeddedsChanged {
			return t, nil
		}
		return types.NewInterfaceType(methods, embeddeds), nil
	case *types.Map:
		k := t.Key()
		k1, err := walk(k)
		if err != nil {
			return nil, err
		}
		v := t.Elem()
		v1, err := walk(v)
		if err != nil {
			return nil, err
		}
		if k1 == k && v1 == v {
			return t, nil
		}
		return types.NewMap(k1, v1), nil
	case *types.Chan:
		v := t.Elem()
		v1, err := walk(v)
		if err != nil {
			return nil, err
		}
		if v1 == v {
			return t, nil
		}
		return types.NewChan(t.Dir(), v1), nil
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

// WalkModify is exactly like [WalkModifyErr], but without the ability
// to return an error.
func WalkModify(t types.Type, fn func(t types.Type) types.Type) types.Type {
	res, _ := WalkModifyErr(t, func(t types.Type) (types.Type, error) {
		return fn(t), nil
	})
	return res
}
