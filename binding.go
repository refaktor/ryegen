package main

import (
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

func collectImports(t types.Type) []*types.Package {
	imports := map[string]*types.Package{}
	var doCollectImports func(t types.Type)
	doCollectImports = func(t types.Type) {
		if t, ok := t.(*types.Named); ok && t.Obj().Exported() {
			if pkg := t.Obj().Pkg(); pkg != nil {
				imports[pkg.Path()] = pkg
			}
		}
		walktypes.Walk(t, doCollectImports)
	}
	doCollectImports(t)
	return slices.Collect(maps.Values(imports))
}

type binding struct {
	// Receiver type in Rye
	recv string
	// Go code resulting in the func to be converted
	funcCode string

	// Package of the func/type
	pkg *types.Package
	// A converter to Rye for this signature type is required
	// for the binding
	requiredConverter *types.Signature
	// Imports required by the binding code (order and element uniqueness not guaranteed)
	requiredImports []*types.Package

	name    string // binding name in Rye
	exclude bool   // true -> don't generate
}

func newFuncBinding(f *types.Func, qualifier types.Qualifier) binding {
	signature := f.Signature()

	var bf binding
	bf.pkg = f.Pkg()
	bf.requiredConverter = signature
	bf.requiredImports = []*types.Package{f.Pkg()}
	if f.Pkg().Path() == "golang.org/x/crypto/cryptobyte" && f.Name() == "ReadOptionalASN1Boolean" {
		// TODO: For some reason, the type checker gives us the wrong type here:
		// `func (*golang.org/x/crypto/cryptobyte.String).ReadOptionalASN1Boolean(*bool, bool) bool`
		// instead of `func (*golang.org/x/crypto/cryptobyte.String).ReadOptionalASN1Boolean(*bool, golang.org/x/crypto/cryptobyte/asn1.Tag, bool) bool`
		// I have absolutely no idea why this happens.
		bf.exclude = true
		return bf
	}

	bf.name = f.Name()
	var fun string
	if signature.Recv() == nil {
		if pkg := f.Pkg(); pkg.Path() != "" {
			if pkg := qualifier(pkg); pkg != "" {
				fun = pkg + "."
			}
		}
		fun += f.Name()
	} else {
		bf.requiredImports = append(bf.requiredImports, signature.Recv().Pkg())
		bf.recv = converter.ReceiverRyeTypeName(signature.Recv().Type(), qualifier)
		fun = fmt.Sprintf("(%v).%v", types.TypeString(signature.Recv().Type(), qualifier), f.Name())
	}
	bf.funcCode = fun
	return bf
}

func newConstructorBinding(typ *types.Named, qualifier types.Qualifier) binding {
	signature := types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "", typ)),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", typ)),
		false,
	)
	return binding{
		funcCode: fmt.Sprintf(`func(v %v) %v { return v }`,
			types.TypeString(typ, qualifier),
			types.TypeString(typ, qualifier),
		),
		name:              typ.Obj().Name(),
		pkg:               typ.Obj().Pkg(),
		requiredConverter: signature,
		requiredImports:   []*types.Package{typ.Obj().Pkg()},
	}
}

func newGetterBindings(typ *types.Named, qualifier types.Qualifier) []binding {
	struc, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return nil
	}

	var bindings []binding
	for field := range struc.Fields() {
		if !field.Exported() {
			continue
		}
		maybeAddrStr := ""
		returnType := field.Type()
		if _, ok := returnType.Underlying().(*types.Struct); ok {
			// If the getter returns a struct, we want to return a pointer,
			// so that the returned value is addressable. This way, it's possible
			// to manipulate nested structs.
			returnType = types.NewPointer(returnType)
			maybeAddrStr = "&"
		}
		signature := types.NewSignatureType(
			nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.NewPointer(typ))),
			types.NewTuple(types.NewVar(token.NoPos, nil, "", returnType)),
			false,
		)
		bindings = append(bindings, binding{
			recv: converter.ReceiverRyeTypeName(typ, qualifier),
			funcCode: fmt.Sprintf(`func(s *%v) %v { return %vs.%v }`,
				types.TypeString(typ, qualifier),
				types.TypeString(returnType, qualifier),
				maybeAddrStr,
				field.Name(),
			),
			name:              field.Name() + "?",
			pkg:               typ.Obj().Pkg(),
			requiredConverter: signature,
			requiredImports: append([]*types.Package{typ.Obj().Pkg()},
				collectImports(field.Type())...),
		})
	}
	return bindings
}

func newSetterBindings(typ *types.Named, qualifier types.Qualifier) []binding {
	struc, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return nil
	}

	var bindings []binding
	for field := range struc.Fields() {
		if !field.Exported() {
			continue
		}
		signature := types.NewSignatureType(
			nil, nil, nil,
			types.NewTuple(
				types.NewVar(token.NoPos, nil, "", types.NewPointer(typ)),
				types.NewVar(token.NoPos, nil, "", field.Type()),
			),
			nil,
			false,
		)
		bindings = append(bindings, binding{
			recv: converter.ReceiverRyeTypeName(typ, qualifier),
			funcCode: fmt.Sprintf(`func(s *%v, v %v) { s.%v = v }`,
				types.TypeString(typ, qualifier),
				types.TypeString(field.Type(), qualifier),
				field.Name(),
			),
			name:              field.Name() + "!",
			pkg:               typ.Obj().Pkg(),
			requiredConverter: signature,
			requiredImports: append([]*types.Package{typ.Obj().Pkg()},
				collectImports(field.Type())...),
		})
	}
	return bindings
}

func newMethodBindings(namedTyp *types.Named, qualifier types.Qualifier) []binding {
	var bindings []binding
	recvTyp := types.Type(namedTyp)
	if iface, ok := namedTyp.Underlying().(*types.Interface); ok {
		if !iface.IsMethodSet() {
			// Contains type constraints
			return nil
		}
	} else {
		// Only non-interface can be pointer receiver
		recvTyp = types.NewPointer(namedTyp)
	}
	ms := types.NewMethodSet(recvTyp)
	for m := range ms.Methods() {
		m := m.Obj().(*types.Func)
		if !m.Exported() {
			continue
		}
		{
			// Receiver is currently the receiver the method was declared on.
			// Set receiver to the current type we're actually binding methods for.
			recv := m.Signature().Recv()
			sig := m.Signature()
			m = types.NewFunc(m.Pos(), namedTyp.Obj().Pkg(), m.Name(),
				types.NewSignatureType(
					types.NewVar(recv.Pos(), namedTyp.Obj().Pkg(), "", recvTyp),
					nil, //slices.Collect(sig.RecvTypeParams().TypeParams()),
					nil, //slices.Collect(sig.TypeParams().TypeParams()),
					sig.Params(),
					sig.Results(),
					sig.Variadic(),
				),
			)
		}

		bindings = append(bindings, newFuncBinding(m, qualifier))
	}
	return bindings
}

func (bf *binding) key() string {
	if bf.recv == "" {
		return bf.name
	} else {
		return bf.recv + "//" + bf.name
	}
}

func (bf *binding) binding(convName string) string {
	return fmt.Sprintf("mustBuiltin(%v(nil, %v))", convName, bf.funcCode)
}
