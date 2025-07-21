package main

import (
	"fmt"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/refaktor/ryegen/v2/config"
	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/converter/walktypes"
)

const builtinsCommonCode = `import (
	_env "github.com/refaktor/rye/env"
	_evaldo "github.com/refaktor/rye/evaldo"
	_runner "github.com/refaktor/rye/runner"
)

func mustBuiltin(x _env.VarBuiltin, err error) *_env.VarBuiltin {
	if err != nil {
		panic(err)
	}
	return &x
}

func builtinsContext(ps *_env.ProgramState, builtins map[string]*_env.VarBuiltin, name string) *_env.RyeCtx {
	ctx := ps.Ctx
	ps.Ctx = _env.NewEnv(ps.Ctx)
	_evaldo.RegisterVarBuiltins2(builtins, ps, name)
	newctx := ps.Ctx
	ps.Ctx = ctx
	wordIdx := ps.Idx.IndexWord(name)
	ps.Ctx.Mod(wordIdx, *newctx)
	return newctx
}

var packages = map[string]*_env.RyeCtx{}

func main() {
	_runner.DoMain(func(ps *_env.ProgramState) error {
		for pkg, builtins := range builtins {
			packages[pkg] = builtinsContext(ps, builtins, "gopkg(" + pkg + ")")
		}
		_evaldo.RegisterVarBuiltins2(map[string]*_env.VarBuiltin{
			"nil": {Argsn: 0, Fn: func(ps *_env.ProgramState, _ ..._env.Object) _env.Object { return *_env.NewVoid() }},
			"import\\go": {
				Argsn: 1,
				Fn: func(ps *_env.ProgramState, args ..._env.Object) _env.Object {
					arg0, ok := args[0].(_env.String)
					if !ok {
						return _env.NewError("expected package name string, but got " + objectType(ps, args[0]))
					}
					pkg, ok := packages[arg0.Value]
					if !ok {
						return _env.NewError("unknown Go package \"" + arg0.Value + "\"")
					}
					return *pkg
				},
			},
		}, ps, "base")
		return nil
	})
}

`

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

type binding struct {
	// Receiver type in Rye
	recv string
	// Go code resulting in the func to be converted
	funcCode string
	// Config spec
	spec config.SymbolSpec

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
	bf.spec = config.SymbolSpec{
		Name: bf.name,
		Type: config.SymbolFunc,
	}
	if signature.Recv() != nil {
		bf.spec.Recv = converter.ReceiverTypeNameNoPtr(signature.Recv().Type())
	}
	return bf
}

func newConstructorBinding(typ *types.Named, qualifier types.Qualifier) binding {
	signature := types.NewSignatureType(
		nil, nil, nil,
		types.NewTuple(types.NewVar(token.NoPos, nil, "", typ)),
		types.NewTuple(types.NewVar(token.NoPos, nil, "", typ)),
		false,
	)
	name := typ.Obj().Name()
	return binding{
		funcCode: fmt.Sprintf(`func(v %v) %v { return v }`,
			types.TypeString(typ, qualifier),
			types.TypeString(typ, qualifier),
		),
		spec: config.SymbolSpec{
			Name: name,
			Type: config.SymbolConstructor,
		},
		name:              name,
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
		name := field.Name() + "?"
		bindings = append(bindings, binding{
			recv: converter.ReceiverRyeTypeName(typ, qualifier),
			funcCode: fmt.Sprintf(`func(s *%v) %v { return %vs.%v }`,
				types.TypeString(typ, qualifier),
				types.TypeString(returnType, qualifier),
				maybeAddrStr,
				field.Name(),
			),
			spec: config.SymbolSpec{
				Name: name,
				Recv: converter.ReceiverTypeNameNoPtr(typ),
				Type: config.SymbolGetter,
			},
			name:              name,
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
		name := field.Name() + "!"
		bindings = append(bindings, binding{
			recv: converter.ReceiverRyeTypeName(typ, qualifier),
			funcCode: fmt.Sprintf(`func(s *%v, v %v) { s.%v = v }`,
				types.TypeString(typ, qualifier),
				types.TypeString(field.Type(), qualifier),
				field.Name(),
			),
			spec: config.SymbolSpec{
				Name: name,
				Recv: converter.ReceiverTypeNameNoPtr(typ),
				Type: config.SymbolGetter,
			},
			name:              name,
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
	var b strings.Builder
	if bf.recv != "" {
		fmt.Fprintf(&b, "%v//", bf.recv)
	}
	fmt.Fprintf(&b, "%v", bf.name)
	return b.String()
}

func (bf *binding) binding(convName string) string {
	return fmt.Sprintf("mustBuiltin(%v(nil, %v))", convName, bf.funcCode)
}

func applyBindingRules(c *config.Config, bfs *[]binding) error {
	pkgIdx := map[string]int{}
	var spec []config.PackageSpec
	for _, bf := range *bfs {
		if _, ok := pkgIdx[bf.pkg.Path()]; !ok {
			spec = append(spec, config.PackageSpec{
				PkgPath: bf.pkg.Path(),
			})
			pkgIdx[bf.pkg.Path()] = len(spec) - 1
		}
		syms := &spec[pkgIdx[bf.pkg.Path()]].Symbols
		(*syms) = append((*syms), bf.spec)
	}

	names, included, err := c.ExecuteRules(spec)
	if err != nil {
		return err
	}
	*bfs = slices.DeleteFunc(*bfs, func(bf binding) bool {
		return !included[bf.pkg.Path()][bf.spec.Symbol()]
	})
	for i, bf := range *bfs {
		(*bfs)[i].name = names[bf.pkg.Path()][bf.spec.Symbol()]
	}
	return nil
}
