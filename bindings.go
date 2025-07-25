package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/refaktor/ryegen/v2/config"
	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/converter/typeset"
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
			"nil": {
				Argsn: 0,
				Fn: func(ps *_env.ProgramState, _ ..._env.Object) _env.Object {
					return *_env.NewVoid()
				},
			},
			"is-nil": {
				Argsn: 1,
				Fn: func(ps *_env.ProgramState, objs ..._env.Object) _env.Object {
					_, ok := objs[0].(_env.Void)
					return *_env.NewBoolean(ok)
				},
			},
			"import\\go": {
				Argsn: 1,
				Fn: func(ps *_env.ProgramState, args ..._env.Object) _env.Object {
					arg0, ok := args[0].(_env.String)
					if !ok {
						ps.FailureFlag = true
						return _env.NewError("expected package name string, but got " + objectType(ps, args[0]))
					}
					pkg, ok := packages[arg0.Value]
					if !ok {
						ps.FailureFlag = true
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

type bindingType int

const (
	// Function/method
	bindingFunc bindingType = iota
	// E.g. .FieldName?
	bindingGetter
	// E.g. .FieldName!
	bindingSetter
	// E.g. StructName
	bindingConstructor
)

func (sym bindingType) String() string {
	switch sym {
	case bindingFunc:
		return "func"
	case bindingGetter:
		return "getter"
	case bindingSetter:
		return "setter"
	case bindingConstructor:
		return "constructor"
	default:
		panic("invalid symbol")
	}
}

func bindingTypeFromString(s string) (bindingType, bool) {
	if strings.EqualFold(s, "func") {
		return bindingFunc, true
	} else if strings.EqualFold(s, "getter") {
		return bindingGetter, true
	} else if strings.EqualFold(s, "setter") {
		return bindingSetter, true
	} else if strings.EqualFold(s, "constructor") {
		return bindingConstructor, true
	} else {
		return -1, false
	}
}

type bindingProperties struct {
	pkgPath string // package path in Rye
	recv    string // receiver type in Rye
	name    string // binding name in Rye
	exclude bool   // true -> don't generate
}

type binding struct {
	// Go code resulting in the func to be converted
	funcCode string

	// Binding type
	typ bindingType
	// Go receiver type for textual filtering (without ptrs and struct without aliases)
	recv string
	// Package of the func/type
	pkg *types.Package
	// A converter to Rye for this signature type is required
	// for the binding
	requiredConverter *types.Signature
	// Imports required by the binding code (order and element uniqueness not guaranteed)
	requiredImports []*types.Package

	// Binding properties. Data in here is what's mutated by binding rules.
	props bindingProperties
}

// fillProps sets the props field given a
// default binding name, receiver type and
// type qualifier.
func (b *binding) fillPropsAndRecv(bName string, tset *typeset.TypeSet) {
	b.props = bindingProperties{
		name: bName,
	}
	if b.pkg != nil {
		b.props.pkgPath = b.pkg.Path()
	}
	if b.requiredConverter.Recv() != nil {
		b.props.recv =
			converter.ReceiverRyeTypeName(b.requiredConverter.Recv().Type(), tset)
		b.recv = recvTypeNameForTextualFiltering(b.requiredConverter.Recv().Type())
	}
}

func newFuncBinding(f *types.Func, tset *typeset.TypeSet) binding {
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

func newConstructorBinding(typ *types.Named, tset *typeset.TypeSet) binding {
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

func newFieldGetterBindings(typ types.Type, tset *typeset.TypeSet) []binding {
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

func newFieldSetterBindings(typ types.Type, tset *typeset.TypeSet) []binding {
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

func newGlobalGetterBinding(obj types.Object, tset *typeset.TypeSet) binding {
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

func newGlobalSetterBinding(obj types.Object, tset *typeset.TypeSet) binding {
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

func newMethodBindings(namedTyp *types.Named, tset *typeset.TypeSet) []binding {
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

		bindings = append(bindings, newFuncBinding(m, tset))
	}
	return bindings
}

func (bf *binding) key() string {
	var b strings.Builder
	if bf.props.recv != "" {
		fmt.Fprintf(&b, "%v//", bf.props.recv)
	}
	fmt.Fprintf(&b, "%v", bf.props.name)
	return b.String()
}

func (bf *binding) binding(convName string) string {
	return fmt.Sprintf("mustBuiltin(%v(nil, %v))", convName, bf.funcCode)
}

func applyBindingRules(c *config.Config, bfs *[]binding) (err error) {
	type localSymbolID struct {
		recv string
		name string
	}

	type renameUsage int
	const (
		renameRename renameUsage = iota
		renameCasing
		renamePkg
	)

	bfIdxs := map[string]map[localSymbolID]int{} // current package -> current ID -> index into bfs
	initialProps := make([]bindingProperties, len(*bfs))
	for bfIdx, bf := range *bfs {
		if _, ok := bfIdxs[bf.props.pkgPath]; !ok {
			bfIdxs[bf.props.pkgPath] = map[localSymbolID]int{}
		}
		bfIdxs[bf.props.pkgPath][localSymbolID{bf.recv, bf.props.name}] = bfIdx
		initialProps[bfIdx] = bf.props
	}

	// Backrefs represents the '\1', '\2' etc.,
	// which are created by making a capture
	// group in the package and/or name selector.
	// We split a single slice of []byte into two
	// sections for the package and name part to
	// reduce memory allocations.
	var backrefs [][]byte

	for _, rule := range c.Rules {
		for bfIdx, bf := range *bfs {
			backrefs = backrefs[:0]

			if rule.Select.Package != nil {
				m := rule.Select.Package.FindSubmatch([]byte(bf.props.pkgPath))
				if len(m) == 0 || len(m[0]) != len(bf.props.pkgPath) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
			}
			if rule.Select.Type != "" {
				if _, ok := bindingTypeFromString(rule.Select.Type); !ok {
					return fmt.Errorf("select: unknown symbol type: %v", rule.Select.Type)
				}
				if !strings.EqualFold(rule.Select.Type, bf.typ.String()) {
					continue
				}
			}
			if rule.Select.Recv != nil {
				m := rule.Select.Recv.FindSubmatch([]byte(bf.recv))
				if len(m) == 0 || len(m[0]) != len(bf.recv) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
			}
			if rule.Select.Name != nil {
				m := rule.Select.Name.FindSubmatch([]byte(bf.props.name))
				if len(m) == 0 || len(m[0]) != len(bf.props.name) {
					continue
				}
				backrefs = append(backrefs, m[1:]...)
			}

			doRename := func(newName, newPkgPath string, usage renameUsage) error {
				if usage == renamePkg {
					if newPkgPath == "" {
						return fmt.Errorf("setting package would cause \"(%v).%v\"'s package path to become empty, which is not allowed",
							bf.props.pkgPath, bf.props.name)
					}
					newName = bf.props.name // keep name
				} else {
					if newName == "" {
						return fmt.Errorf("rename would cause \"(%v).%v\"'s name to become empty, which is not allowed",
							bf.props.pkgPath, bf.props.name)
					}
					newPkgPath = bf.props.pkgPath // keep pkg
				}
				if newName == bf.props.name && newPkgPath == bf.props.pkgPath {
					return nil
				}

				newSym := localSymbolID{bf.recv, newName}
				if _, ok := bfIdxs[newPkgPath]; !ok {
					bfIdxs[newPkgPath] = map[localSymbolID]int{}
				} else if conflictIdx, exists := bfIdxs[newPkgPath][newSym]; exists && !(*bfs)[conflictIdx].props.exclude {
					conflict := (*bfs)[conflictIdx]
					var fullNewName string
					if newPkgPath != bf.props.pkgPath {
						fullNewName = "(" + newPkgPath + ")."
					}
					fullNewName += newName
					var targetName string
					if usage == renamePkg {
						targetName = newPkgPath
					} else {
						targetName = fullNewName
					}
					var originallyText string
					if init := initialProps[conflictIdx]; conflict.props.name != init.name ||
						conflict.props.pkgPath != init.pkgPath {
						originallyText = fmt.Sprintf(" (originally \"(%v).%v\")", init.pkgPath, init.name)
					}
					var errPfx string
					switch usage {
					case renameRename:
						errPfx = "renaming"
					case renameCasing:
						errPfx = "to-casing: renaming"
					case renamePkg:
						errPfx = "setting package of"
					}
					fmt.Println("recv:", bf.recv)
					return fmt.Errorf("%v %v \"(%v).%v\" to \"%v\" would cause a naming conflict with %v \"%v\"%v",
						errPfx, bf.typ, bf.props.pkgPath, bf.props.name, targetName, conflict.typ, fullNewName, originallyText)
				}

				bfIdxs[newPkgPath][newSym] = bfIdx
				delete(bfIdxs[bf.props.pkgPath], localSymbolID{bf.recv, bf.props.name})
				bf.props.name = newName
				bf.props.pkgPath = newPkgPath
				(*bfs)[bfIdx] = bf

				return nil
			}

			substBackrefs := func(s string) string {
				oldnew := [2 * 9]string{
					`\1`, "",
					`\2`, "",
					`\3`, "",
					`\4`, "",
					`\5`, "",
					`\6`, "",
					`\7`, "",
					`\8`, "",
					`\9`, "",
				}
				for i := range min(len(backrefs), 9) {
					oldnew[2*i+1] = string(backrefs[i])
				}
				return strings.NewReplacer(oldnew[:]...).
					Replace(s)
			}

			if rule.Actions.Include != nil {
				(*bfs)[bfIdx].props.exclude = !*rule.Actions.Include
				bf = (*bfs)[bfIdx]
			}

			if !bf.props.exclude {
				if rule.Actions.Rename != "" {
					newName := substBackrefs(rule.Actions.Rename)
					if err := doRename(newName, "", renameRename); err != nil {
						return err
					}
				}

				if rule.Actions.ToCasing != "" {
					var newName string
					switch rule.Actions.ToCasing {
					case "kebab":
						newName = strcase.ToKebab(bf.props.name)
					case "camel":
						newName = strcase.ToCamel(bf.props.name)
					case "snake":
						newName = strcase.ToSnake(bf.props.name)
					default:
						return fmt.Errorf("action: unknown casing: %v", rule.Actions.ToCasing)
					}
					if err := doRename(newName, "", renameCasing); err != nil {
						return err
					}
				}

				if rule.Actions.SetPackage != "" {
					newPkgPath := substBackrefs(rule.Actions.SetPackage)
					if err := doRename("", newPkgPath, renamePkg); err != nil {
						return err
					}
				}
			}
		}
	}

	*bfs = slices.DeleteFunc(*bfs, func(bf binding) bool { return bf.props.exclude })

	return nil
}

func addFileBindings(bindings []binding, tset *typeset.TypeSet, pkg *types.Package, typesInfo *types.Info, files []*ast.File) []binding {
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
				bindings = append(bindings, newFuncBinding(f, tset))
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
							bindings = append(bindings, newMethodBindings(namedTyp, tset)...)
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
								bindings = append(bindings, newGlobalGetterBinding(obj, tset))
								if decl.Tok == token.VAR {
									bindings = append(bindings, newGlobalSetterBinding(obj, tset))
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

		bindings = append(bindings, newConstructorBinding(typ, tset))
		bindings = append(bindings, newFieldGetterBindings(typ, tset)...)
		bindings = append(bindings, newFieldSetterBindings(typ, tset)...)
	}
	for _, name := range slices.Sorted(maps.Keys(structAliasTypes)) {
		alias := structAliasTypes[name]
		getters := newFieldGetterBindings(alias, tset)
		setters := newFieldSetterBindings(alias, tset)
		bindings = append(bindings, getters...)
		bindings = append(bindings, setters...)
	}

	return bindings
}
