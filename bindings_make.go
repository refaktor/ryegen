package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"github.com/refaktor/ryegen/v2/converter/typeset"
)

func makeFuncBinding(f *types.Func, tset *typeset.TypeSet) binding {
	signature := f.Signature()

	var bf binding
	bf.typ = bindingFunc
	bf.pkg = f.Pkg()
	bf.requiredConverter = signature
	bf.requiredImports = []*types.Package{f.Pkg()}

	var fun string
	if signature.Recv() == nil {
		if pkg := f.Pkg(); pkg.Path() != "" {
			if pkg := tset.Qualifier()(pkg); pkg != "" {
				fun = pkg + "."
			}
		}
		fun += f.Name()
	} else {
		bf.requiredImports = append(bf.requiredImports, signature.Recv().Pkg())
		fun = fmt.Sprintf("(%v).%v", tset.TypeString(signature.Recv().Type()), f.Name())
	}
	bf.funcCode = fun

	bf.fillPropsAndRecv(f.Name(), tset)

	if f.Pkg().Path() == "golang.org/x/crypto/cryptobyte" && f.Name() == "ReadOptionalASN1Boolean" {
		// TODO: For some reason, the type checker gives us the wrong type here:
		// `func (*golang.org/x/crypto/cryptobyte.String).ReadOptionalASN1Boolean(*bool, bool) bool`
		// instead of `func (*golang.org/x/crypto/cryptobyte.String).ReadOptionalASN1Boolean(*bool, golang.org/x/crypto/cryptobyte/asn1.Tag, bool) bool`
		// I have absolutely no idea why this happens.
		bf.props.exclude = true
		return bf
	}

	return bf
}

func makeConstructorBinding(typ *types.Named, tset *typeset.TypeSet) binding {
	signature := types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "", typ)),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", typ)),
		false,
	)
	bf := binding{
		typ: bindingConstructor,
		funcCode: fmt.Sprintf(`func(v %v) %v { return v }`,
			tset.TypeString(typ),
			tset.TypeString(typ),
		),
		pkg:               typ.Obj().Pkg(),
		requiredConverter: signature,
		requiredImports:   []*types.Package{typ.Obj().Pkg()},
	}
	bf.fillPropsAndRecv(typ.Obj().Name(), tset)
	return bf
}

func makeFieldGetterBindings(typ types.Type, tset *typeset.TypeSet) []binding {
	var pkg *types.Package
	switch t := typ.(type) {
	case *types.Alias:
		pkg = t.Obj().Pkg()
	case *types.Named:
		pkg = t.Obj().Pkg()
	default:
		return nil
	}
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
			types.NewVar(token.NoPos, nil, "", types.NewPointer(typ)),
			nil, nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "", returnType)),
			false,
		)

		var requiredImports []*types.Package
		if pkg != nil {
			requiredImports = append(requiredImports, pkg)
		}

		requiredImports = append(requiredImports, collectImports(field.Type())...)
		bf := binding{
			typ: bindingGetter,
			funcCode: fmt.Sprintf(`func(s *%v) %v { return %vs.%v }`,
				tset.TypeString(typ),
				tset.TypeString(returnType),
				maybeAddrStr,
				field.Name(),
			),
			pkg:               pkg,
			requiredConverter: signature,
			requiredImports:   requiredImports,
		}
		bf.fillPropsAndRecv(field.Name()+"?", tset)
		bindings = append(bindings, bf)
	}
	return bindings
}

func makeFieldSetterBindings(typ types.Type, tset *typeset.TypeSet) []binding {
	var pkg *types.Package
	switch t := typ.(type) {
	case *types.Alias:
		pkg = t.Obj().Pkg()
	case *types.Named:
		pkg = t.Obj().Pkg()
	default:
		return nil
	}
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
			types.NewVar(token.NoPos, nil, "", types.NewPointer(typ)),
			nil, nil,
			types.NewTuple(types.NewVar(token.NoPos, nil, "", field.Type())),
			types.NewTuple(types.NewVar(token.NoPos, nil, "", types.NewPointer(typ))),
			false,
		)

		var requiredImports []*types.Package
		if pkg != nil {
			requiredImports = append(requiredImports, pkg)
		}
		requiredImports = append(requiredImports, collectImports(field.Type())...)

		bf := binding{
			typ: bindingSetter,
			funcCode: fmt.Sprintf(`func(s *%v, v %v) *%v { s.%v = v; return s }`,
				tset.TypeString(typ),
				tset.TypeString(field.Type()),
				tset.TypeString(typ),
				field.Name(),
			),
			pkg:               pkg,
			requiredConverter: signature,
			requiredImports:   requiredImports,
		}
		bf.fillPropsAndRecv(field.Name()+"!", tset)
		bindings = append(bindings, bf)
	}
	return bindings
}

func makeGlobalGetterBinding(obj types.Object, tset *typeset.TypeSet) binding {
	maybeAddrStr := ""
	returnType := resolveUntyped(obj) // consts may be untyped
	if _, ok := returnType.Underlying().(*types.Struct); ok {
		// If the getter returns a struct, we want to return a pointer,
		// so that the returned value is addressable. This way, it's possible
		// to manipulate nested structs.
		returnType = types.NewPointer(returnType)
		maybeAddrStr = "&"
	}
	signature := types.NewSignatureType(
		nil, nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "", returnType)),
		false,
	)
	bf := binding{
		typ: bindingGetter,
		funcCode: fmt.Sprintf(`func() %v { return %v%v }`,
			tset.TypeString(returnType),
			maybeAddrStr,
			objectString(obj, tset.Qualifier()),
		),
		pkg:               obj.Pkg(),
		requiredConverter: signature,
		requiredImports:   []*types.Package{obj.Pkg()},
	}
	bf.fillPropsAndRecv(obj.Name()+"?", tset)
	return bf
}

func makeGlobalSetterBinding(obj types.Object, tset *typeset.TypeSet) binding {
	signature := types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "", obj.Type())),
		nil,
		false,
	)
	bf := binding{
		typ: bindingSetter,
		funcCode: fmt.Sprintf(`func(x %v) { %v = x }`,
			tset.TypeString(obj.Type()),
			objectString(obj, tset.Qualifier()),
		),
		pkg:               obj.Pkg(),
		requiredConverter: signature,
		requiredImports:   []*types.Package{obj.Pkg()},
	}
	bf.fillPropsAndRecv(obj.Name()+"!", tset)
	return bf
}

func makeMethodBindings(namedTyp *types.Named, tset *typeset.TypeSet) []binding {
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

		bindings = append(bindings, makeFuncBinding(m, tset))
	}
	return bindings
}

func makePkgBindings(tset *typeset.TypeSet, typesInfo *types.Info, files []*ast.File) []binding {
	var bindings []binding
	namedTypes := map[string]*types.Named{}
	structAliasTypes := map[string]*types.Alias{}
	for _, f := range files {
		for _, decl := range f.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() {
					continue
				}
				if decl.Recv != nil {
					// Methods aren't handled here
					continue
				}

				f := typesInfo.ObjectOf(decl.Name).(*types.Func)
				addStructAliasTypes(structAliasTypes, tset, tset.Normalized(f.Signature()))
				bindings = append(bindings, makeFuncBinding(f, tset))
			case *ast.GenDecl:
				switch decl.Tok {
				case token.TYPE:
					for _, spec := range decl.Specs {
						if spec, ok := spec.(*ast.TypeSpec); ok {
							if !spec.Name.IsExported() {
								continue
							}
							namedTyp, ok := typesInfo.ObjectOf(spec.Name).Type().(*types.Named)
							if !ok {
								// Alias type (doesn't have any methods)
								continue
							}
							if namedTyp.TypeParams() != nil {
								continue
							}
							if iface, ok := namedTyp.Underlying().(*types.Interface); ok {
								if !iface.IsMethodSet() {
									// Contains type constraints
									continue
								}
							}
							for m := range namedTyp.Methods() {
								addStructAliasTypes(structAliasTypes, tset, tset.Normalized(m.Signature()))
							}
							bindings = append(bindings, makeMethodBindings(namedTyp, tset)...)
							namedTypes[spec.Name.Name] = typesInfo.ObjectOf(spec.Name).Type().(*types.Named)
						}
					}
				case token.CONST, token.VAR:
					for _, spec := range decl.Specs {
						if spec, ok := spec.(*ast.ValueSpec); ok {
							for _, name := range spec.Names {
								if !name.IsExported() {
									continue
								}
								obj := typesInfo.ObjectOf(name)
								addStructAliasTypes(structAliasTypes, tset, tset.Normalized(obj.Type()))
								bindings = append(bindings, makeGlobalGetterBinding(obj, tset))
								if decl.Tok == token.VAR {
									bindings = append(bindings, makeGlobalSetterBinding(obj, tset))
								}
							}
						}
					}
				}
			}
		}
	}

	for _, typName := range slices.Sorted(maps.Keys(namedTypes)) {
		typ := namedTypes[typName]

		bindings = append(bindings, makeConstructorBinding(typ, tset))
		bindings = append(bindings, makeFieldGetterBindings(typ, tset)...)
		bindings = append(bindings, makeFieldSetterBindings(typ, tset)...)
	}
	for _, name := range slices.Sorted(maps.Keys(structAliasTypes)) {
		alias := structAliasTypes[name]
		getters := makeFieldGetterBindings(alias, tset)
		setters := makeFieldSetterBindings(alias, tset)
		bindings = append(bindings, getters...)
		bindings = append(bindings, setters...)
	}

	return bindings
}
