package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/module"
	"github.com/refaktor/ryegen/v2/moduleset"
	rg_parser "github.com/refaktor/ryegen/v2/parser"
	"github.com/refaktor/ryegen/v2/repo"
	"golang.org/x/mod/semver"
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
			"go\\": {
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

type bindingFunc struct {
	exclude  bool
	name     string
	fn       *types.Func
	convName string
}

func newBindingFunc(f *types.Func) (bindingFunc, converter.ConverterSpec) {
	typ := f.Type()
	spec := converter.NewSpec(
		typ,
		converter.ToRye,
	)
	return bindingFunc{
		name:     f.Name(),
		fn:       f,
		convName: spec.Name(),
	}, spec
}

// Returns the builtin name and value code.
// bindingKey is the builtin name, which it should be registered under within Rye.
// builtin is Go code evaluating to the *env.VarBuiltin value.
func (fn *bindingFunc) builtin(qualifier types.Qualifier) (bindingKey string, builtin string) {
	signature := fn.fn.Signature()
	var fun string
	if signature.Recv() == nil {
		bindingKey = fn.name
		if pkg := fn.fn.Pkg(); pkg.Path() != "" {
			fun = qualifier(pkg) + "."
		}
		fun += fn.fn.Name()
	} else {
		recv := types.TypeString(signature.Recv().Type(), qualifier)
		{
			under := signature.Recv().Type().Underlying()
			if _, ok := under.(*types.Pointer); !ok && !types.IsInterface(under) {
				// Non-pointer, non-interface receiver should always be a pointer.
				recv = "*" + recv
			}
		}
		bindingKey = "go(" + recv + ")//" + fn.name

		fun = fmt.Sprintf("(%v).%v", types.TypeString(signature.Recv().Type(), qualifier), fn.fn.Name())
	}
	return bindingKey, fmt.Sprintf(`mustBuiltin(%v(nil, %v))`, fn.convName, fun)
}

// Removes all indirections of t.
func unpointer(t types.Type) types.Type {
	for {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		} else {
			return t
		}
	}
}

type importer struct {
	fset         *token.FileSet
	ms           *moduleset.ModuleSet
	packageNames map[string]string // package path to name
	bindingFuncs []bindingFunc
	convs        map[string]string                  // converters by converter func name
	namedTyps    map[string]map[string]*types.Named // package and name to named type
	packages     map[string]*types.Package          // packages by import path
	imports      map[string]struct{}                // imports required for convs
}

func (ip *importer) Import(impPath string) (_ *types.Package, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("import %v: %w", impPath, err)
		}
	}()

	if impPath == "unsafe" {
		return types.Unsafe, nil
	}
	if impPath == "golang.org/x/sys/internal/unsafeheader" {
		// TODO: Maybe, adding +incompatible support will fix this.
		// The reason for this is that older versions of x/sys do
		// contain this subdirectory, but newer ones don't.
		return ip.Import("internal/unsafeheader")
	}

	if pkg, ok := ip.packages[impPath]; ok {
		return pkg, nil
	}

	srcDir, err := ip.ms.PkgSrcDir(impPath)
	if err != nil {
		return nil, fmt.Errorf("unable to find source path for package %v: %w", impPath, err)
	}
	parsedPkg, err := rg_parser.ParsePackage(ip.fset, srcDir, impPath)
	if err != nil {
		return nil, err
	}
	for _, f := range parsedPkg.Files {
		if err := preprocess(ip.fset, f, func(path string) (string, error) {
			if path == "C" {
				return "C", nil
			}
			pkg, ok := ip.packageNames[path]
			if !ok {
				return "", fmt.Errorf("unable to find package with path %v", path)
			}
			return pkg, nil
		}); err != nil {
			return nil, err
		}
	}
	conf := &types.Config{
		Context:          types.NewContext(),
		GoVersion:        "go1.23",
		IgnoreFuncBodies: true,
		Importer:         ip,
		FakeImportC:      true,
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	pkg, err := conf.Check(impPath, ip.fset, slices.Collect(maps.Values(parsedPkg.Files)), info)
	if err != nil {
		return nil, err
	}
	ip.packages[impPath] = pkg

	var generateConv func(spec converter.ConverterSpec, stack *[]converter.ConverterSpec) error
	generateConv = func(spec converter.ConverterSpec, stack *[]converter.ConverterSpec) error {
		if slices.Contains(*stack, spec) {
			// Dependency cycle, e.g. a struct containing a pointer to itself.
			return nil
		}
		*stack = append(*stack, spec)
		defer func() {
			*stack = (*stack)[:len(*stack)-1]
		}()

		//fmt.Println(spec.Type.String())
		name := spec.Name()
		if _, ok := ip.convs[name]; ok {
			return nil
		}
		if typ, ok := unpointer(spec.Type()).(*types.Named); ok && typ.Obj().Pkg() != nil {
			pkg := typ.Obj().Pkg().Path()
			if ip.namedTyps[pkg] == nil {
				ip.namedTyps[pkg] = map[string]*types.Named{}
			}
			ip.namedTyps[pkg][typ.Obj().Name()] = typ
		}
		text, dependencies, err := spec.Generate()
		if err != nil {
			return err
		}
		for _, dep := range dependencies.Converters {
			if err := generateConv(dep, stack); err != nil {
				return err
			}
		}
		ip.convs[name] = text
		for _, imp := range dependencies.Imports {
			ip.imports[imp.Path()] = struct{}{}
		}
		return nil
	}
	requiresCGo := func(f *ast.FuncDecl) bool {
		requiresCGo := false
		ast.Walk(visitFn(func(node ast.Node) {
			se, ok := node.(*ast.SelectorExpr)
			if !ok {
				return
			}
			id, ok := se.X.(*ast.Ident)
			if !ok {
				return
			}
			if id.Name == "C" {
				requiresCGo = true
			}
		}), f)
		return requiresCGo
	}
	if !slices.Contains(strings.Split(impPath, "/"), "internal") {
		for _, f := range parsedPkg.Files {
			for _, decl := range f.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if !decl.Name.IsExported() || requiresCGo(decl) {
						continue
					}
					if decl.Recv != nil {
						// Methods are handled elsewhere
						continue
					}

					bf, spec := newBindingFunc(info.ObjectOf(decl.Name).(*types.Func))
					if err := generateConv(spec, &[]converter.ConverterSpec{}); err != nil {
						fmt.Printf("err: %v.%v: %v\n", impPath, decl.Name.Name, err)
						continue
						//return nil, err
					}
					ip.bindingFuncs = append(ip.bindingFuncs, bf)
				case *ast.GenDecl:
					if decl.Tok == token.TYPE {
						for _, spec := range decl.Specs {
							if spec, ok := spec.(*ast.TypeSpec); ok {
								if !spec.Name.IsExported() {
									continue
								}
								namedTyp, ok := info.ObjectOf(spec.Name).Type().(*types.Named)
								if !ok {
									// Alias type (doesn't have any methods)
									continue
								}
								var typs []types.Type
								if _, ok := spec.Type.(*ast.InterfaceType); ok {
									typs = []types.Type{namedTyp}
								} else {
									// Only non-interface can be pointer receiver
									typs = []types.Type{namedTyp, types.NewPointer(namedTyp)}
								}
								for _, typ := range typs {
									ms := types.NewMethodSet(typ)
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
													types.NewVar(recv.Pos(), namedTyp.Obj().Pkg(), "", typ),
													nil, //slices.Collect(sig.RecvTypeParams().TypeParams()),
													nil, //slices.Collect(sig.TypeParams().TypeParams()),
													sig.Params(),
													sig.Results(),
													sig.Variadic(),
												),
											)
										}
										bf, spec := newBindingFunc(m)
										if err := generateConv(spec, &[]converter.ConverterSpec{}); err != nil {
											fmt.Printf("err: %v.%v.%v: %v\n", impPath, namedTyp.Obj().Name(), m.Name(), err)
											continue
											//return nil, err
										}
										ip.bindingFuncs = append(ip.bindingFuncs, bf)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return pkg, nil
}

func main() {
	flag.Usage = func() {
		const msg = `
Usage:
  {name} MODULE@VERSION...
Examples:
  {name} example.com/module@v1.0.0
  {name} example.com/module/v2@latest
  {name} example.com/module1@v1.0.0 example.com/module2@v1.2.0
Flags:
`
		fmt.Fprintln(os.Stderr, strings.ReplaceAll(
			strings.TrimSpace(msg),
			"{name}", filepath.Base(os.Args[0]),
		))
		flag.PrintDefaults()
	}

	flag.Parse()
	optModules := flag.Args()

	tStart := time.Now()
	defer func() {
		fmt.Println("time:", time.Since(tStart))
	}()

	if len(optModules) == 0 {
		fmt.Fprintln(os.Stderr, "No modules specified!")
		flag.Usage()
		os.Exit(1)
	} else if len(optModules) > 1 {
		fmt.Fprintln(os.Stderr, "Multiple modules aren't supported yet.")
		flag.Usage()
		os.Exit(1)
	}

	var modulePath, moduleVersion string
	modulePath, moduleVersion, _ = strings.Cut(optModules[0], "@")
	if moduleVersion == "latest" {
		latest, err := repo.GoModuleGetLatestVersion(modulePath)
		if err != nil {
			log.Fatal(err)
		}
		moduleVersion = latest
	}
	if validVer := semver.IsValid(moduleVersion); !validVer || modulePath == "" {
		var err error
		if modulePath == "" {
			err = errors.New("no module specified")
		} else if moduleVersion == "" {
			err = errors.New("no version specified")
		} else if !validVer {
			err = fmt.Errorf("invalid version: %v", moduleVersion)
		}
		fmt.Fprintf(os.Stderr, "Invalid module: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	//{
	//	f, err := os.Create("cpu.prof")
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//	runtime.SetCPUProfileRate(500)
	//	if err := pprof.StartCPUProfile(f); err != nil {
	//		log.Fatal(err)
	//	}
	//	/*go func() {
	//		time.Sleep(time.Second * 5)
	//		pprof.StopCPUProfile()
	//		os.Exit(1)
	//	}()*/
	//	defer pprof.StopCPUProfile()
	//}

	log.SetFlags(log.Llongfile)

	ms, err := moduleset.NewWithCacheFile("_ryegen_src", "ryegen_modcache.gob")
	if err != nil {
		log.Fatal(err)
	}
	ms.OnDownload = func(pkg module.Module, status moduleset.Status) {
		switch status {
		case moduleset.StatusDownloadStarted:
			log.Println("downloading", pkg)
		case moduleset.StatusDownloadFinished:
			log.Println("done")
		}
	}
	if err := ms.Fetch(module.New(modulePath, moduleVersion)); err != nil {
		log.Fatal(err)
	}
	if err := ms.FetchGo(); err != nil {
		log.Fatal(err)
	}
	if err := ms.SaveCacheToFile("ryegen_modcache.json"); err != nil {
		log.Fatal(err)
	}
	if err := ms.SaveCacheToFile("ryegen_modcache.gob"); err != nil {
		log.Fatal(err)
	}

	pkgs := ms.PackageBuildList(module.New(modulePath, moduleVersion))
	imp := &importer{
		fset:         token.NewFileSet(),
		ms:           ms,
		packageNames: map[string]string{},
		convs:        map[string]string{},
		namedTyps:    map[string]map[string]*types.Named{},
		imports:      map[string]struct{}{},
		packages:     map[string]*types.Package{},
	}
	delete(pkgs, "C") // don't import CGo pseudo-package, since it doesn't exist as source code

	fmt.Println(pkgs)

	/*if hg, ok := pkgs["golang.org/x/net/http/httpguts"]; ok {
		fmt.Println(hg)
	} else {
		log.Fatal("httpguts not found")
	}*/

	for _, pkg := range pkgs {
		name, ok := ms.PackageNames[pkg]
		if !ok {
			log.Fatal("no name for package ", pkg)
		}
		imp.packageNames[pkg.Path] = name
	}

	for _, pkg := range pkgs {
		_, err = imp.Import(pkg.Path)
		if err != nil {
			log.Fatal(err)
		}
	}

	packagePathToImportName := func(path string) string {
		return strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(path)
	}
	writeImports := func(w io.Writer, packagePaths []string) {
		fmt.Fprintf(w, "import (\n")
		for _, imp := range packagePaths {
			fmt.Fprintf(w, "\t%v \"%v\"\n", packagePathToImportName(imp), imp)
		}
		fmt.Fprintf(w, ")\n\n")
	}

	codeGeneratedLine := fmt.Sprintf("// Code generated by ryegen %v; DO NOT EDIT.\n", strings.Join(os.Args[1:], " "))

	{
		var out bytes.Buffer
		out.WriteString(codeGeneratedLine)
		out.WriteString("package main\n\n")
		writeImports(&out, slices.Sorted(maps.Keys(imp.imports)))
		out.WriteString(converter.InitCode)
		out.WriteString("\n")
		for _, k := range slices.Sorted(maps.Keys(imp.convs)) {
			conv := imp.convs[k]
			out.WriteString(conv)
			out.WriteString("\n\n")
		}
		if err := os.WriteFile("ryegen_convs.go", out.Bytes(), 0666); err != nil {
			log.Fatal(err)
		}
	}
	{
		bindingFuncImports := map[string]struct{}{}
		packageToBindingFuncs := map[string]map[string]bindingFunc{} // package to func name to bindingFunc
		for _, fn := range imp.bindingFuncs {
			if fn.fn.Pkg() != nil {
				pkg := fn.fn.Pkg().Path()
				if packageToBindingFuncs[pkg] == nil {
					packageToBindingFuncs[pkg] = map[string]bindingFunc{}
				}
				bindingFuncImports[pkg] = struct{}{}
				fnName := fn.fn.Name()
				if recv := fn.fn.Signature().Recv(); recv != nil {
					fnName = recv.Type().String() + "." + fnName
				}
				packageToBindingFuncs[pkg][fnName] = fn
			}
			if fn.fn.Signature().Recv() != nil && fn.fn.Signature().Recv().Pkg() != nil {
				bindingFuncImports[fn.fn.Signature().Recv().Pkg().Path()] = struct{}{}
			}
		}

		var out bytes.Buffer
		out.WriteString(codeGeneratedLine)
		out.WriteString("package main\n\n")
		writeImports(&out, slices.Sorted(maps.Keys(bindingFuncImports)))
		out.WriteString(builtinsCommonCode)
		out.WriteString("var typeLookup = map[string]map[string]nativeTypeEntry{}\n")
		out.WriteString("func init() {\n")
		for _, pkg := range slices.Sorted(maps.Keys(imp.namedTyps)) {
			typs := imp.namedTyps[pkg]
			if pkg == "" {
				pkg = "main"
			}
			fmt.Fprintf(&out, "\t"+`typeLookup["%v"] = map[string]nativeTypeEntry{}`+"\n", pkg)
			for _, name := range slices.Sorted(maps.Keys(typs)) {
				typ := typs[name]
				fmt.Fprintf(&out, "\t"+`typeLookup["%v"]["%v"] = nativeTypeEntry{"%v"}`+"\n", pkg, name, types.TypeString(typ, converter.PkgImportNameQualifier))
			}
		}
		out.WriteString("}\n\n")
		for _, pkg := range slices.Sorted(maps.Keys(packageToBindingFuncs)) {
			bfs := packageToBindingFuncs[pkg]
			mapName := "builtins_" + packagePathToImportName(pkg)
			// HACK: Putting the builtins into a map literal directly will cause a compiler error
			// if there are too many items.
			// E.g.: "internal compiler error: NewBulk too big: nbit=48093 count=589148 nword=1503 size=885489444"
			fmt.Fprintf(&out, "var %v = make(map[string]*_env.VarBuiltin, %v)\n", mapName, len(bfs))
			if len(bfs) > 0 {
				fmt.Fprintf(&out, "func init() {\n")
				fmt.Fprintf(&out, "\t"+`m := %v`+"\n", mapName)
				for _, bf := range slices.Sorted(maps.Keys(bfs)) {
					fn := bfs[bf]
					if fn.exclude {
						continue
					}
					bindingKey, builtin := fn.builtin(converter.PkgImportNameQualifier)
					fmt.Fprintf(&out, "\t"+`m["%v"] = %v`+"\n", bindingKey, builtin)
				}
				fmt.Fprintf(&out, "}\n\n")
			}
		}
		fmt.Fprintf(&out, "var builtins = make(map[string]map[string]*_env.VarBuiltin, %v)\n", len(packageToBindingFuncs))
		fmt.Fprintf(&out, "func init() {\n")
		for _, pkg := range slices.Sorted(maps.Keys(packageToBindingFuncs)) {
			fmt.Fprintf(&out, "\t"+`builtins["%v"] = builtins_%v`+"\n", pkg, packagePathToImportName(pkg))
		}
		out.WriteString("}\n\n")
		err := os.WriteFile("ryegen_builtins.go", out.Bytes(), 0666)
		if err != nil {
			log.Fatal(err)
		}
	}
}
