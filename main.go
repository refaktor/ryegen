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
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"time"

	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/fetcher"
	"github.com/refaktor/ryegen/v2/module"
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

type bindingFunc struct {
	exclude  bool
	name     string
	fn       *types.Func
	convName string
}

func newBindingFunc(f *types.Func, convName string) bindingFunc {
	bf := bindingFunc{
		name:     f.Name(),
		fn:       f,
		convName: convName,
	}
	if f.Pkg().Path() == "golang.org/x/crypto/cryptobyte" && f.Name() == "ReadOptionalASN1Boolean" {
		// TODO: For some reason, the type checker gives us the wrong type here:
		// `func (*golang.org/x/crypto/cryptobyte.String).ReadOptionalASN1Boolean(*bool, bool) bool`
		// instead of `func (*golang.org/x/crypto/cryptobyte.String).ReadOptionalASN1Boolean(*bool, golang.org/x/crypto/cryptobyte/asn1.Tag, bool) bool`
		bf.exclude = true
	}
	return bf
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
			if pkg := qualifier(pkg); pkg != "" {
				fun = pkg + "."
			}
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

type importer struct {
	fetched      *fetcher.Result
	cs           *converter.ConverterSet
	packageNames map[string]string // package path to name
	bindingFuncs []bindingFunc
	packages     map[string]*types.Package // packages by import path
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

	files, ok := ip.fetched.Packages[impPath]
	if !ok {
		return nil, fmt.Errorf("package %v not found", impPath)
	}
	conf := &types.Config{
		Context:          types.NewContext(),
		GoVersion:        ip.fetched.GoVersion,
		IgnoreFuncBodies: true,
		Importer:         ip,
		FakeImportC:      true,
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	pkg, err := conf.Check(impPath, ip.fetched.FileSet, files, info)
	if err != nil {
		return nil, err
	}
	ip.packages[impPath] = pkg

	if !slices.Contains(strings.Split(impPath, "/"), "internal") {
		for _, f := range files {
			for _, decl := range f.Decls {
				switch decl := decl.(type) {
				case *ast.FuncDecl:
					if !decl.Name.IsExported() {
						continue
					}
					if decl.Recv != nil {
						// Methods are handled elsewhere
						continue
					}

					f := info.ObjectOf(decl.Name).(*types.Func)
					name, err := ip.cs.Add(f.Type(), converter.ToRye)
					if err != nil {
						if err != converter.ErrCGo {
							fmt.Printf("err: %v.%v: %v\n", impPath, decl.Name.Name, err)
						}
						continue
					}
					bf := newBindingFunc(f, name)
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

										name, err := ip.cs.Add(m.Type(), converter.ToRye)
										if err != nil {
											if err != converter.ErrCGo {
												fmt.Printf("err: %v.%v.%v: %v\n", impPath, namedTyp.Obj().Name(), m.Name(), err)
											}
											continue
										}
										bf := newBindingFunc(m, name)
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

	var optBuildTags string
	flag.StringVar(&optBuildTags, "tags", "{GOOS},{GOARCH},cgo,gc", "Go build tags separated by comma. \"{GOOS}\" and \"{GOARCH}\" are replaced with host parameters")
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
	}

	var modules []module.Module
	for _, modStr := range optModules {
		path, version, _ := strings.Cut(modStr, "@")
		if version == "latest" {
			latest, err := repo.GoModuleGetLatestVersion(path)
			if err != nil {
				log.Fatal(err)
			}
			version = latest
		}
		if validVer := semver.IsValid(version); !validVer || path == "" {
			var err error
			if path == "" {
				err = errors.New("no module specified")
			} else if version == "" {
				err = errors.New("no version specified")
			} else if !validVer {
				err = fmt.Errorf("invalid version: %v", version)
			}
			fmt.Fprintf(os.Stderr, "Invalid module: %v\n", err)
			flag.Usage()
			os.Exit(1)
		}
		modules = append(modules, module.NewModule(path, version))
	}

	if slices.Contains(
		[]string{"1", "true", "yes"},
		strings.ToLower(os.Getenv("RYEGEN_PROFILE")),
	) {
		const path = "cpu.prof"
		fmt.Println("Ryegen: profiling enabled, writing to", path)
		f, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		//runtime.SetCPUProfileRate(500)
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
		defer fmt.Println("Ryegen: profile saved to", path)
		defer pprof.StopCPUProfile()
	}

	log.SetFlags(log.Llongfile)

	optBuildTags = strings.NewReplacer(
		"{GOOS}", runtime.GOOS,
		"{GOARCH}", runtime.GOARCH,
	).Replace(optBuildTags)
	buildTags := strings.Split(optBuildTags, ",")
	for _, tag := range rg_parser.UnixOSes {
		if slices.Contains(buildTags, tag) &&
			!slices.Contains(buildTags, "unix") {
			buildTags = append(buildTags, "unix")
			break
		}
	}

	fetched, err := fetcher.Fetch(
		"_ryegen",
		modules,
		fetcher.Options{
			CacheFilePath: "_ryegen/ryegen_modcache.gob",
			OnDownloadModule: func(m module.Module) {
				log.Println("downloading", m)
			},
		},
		buildTags,
	)
	if err != nil {
		log.Fatal(err)
	}

	/*
		// Uncomment for debugging
		if err := ms.SaveCacheToFile("ryegen_modcache.json"); err != nil {
			log.Fatal(err)
		}
		if err := ms.SaveCacheToFile("ryegen_modcache.gob"); err != nil {
			log.Fatal(err)
		}*/

	imp := &importer{
		fetched:      fetched,
		cs:           converter.NewConverterSet("main"),
		packageNames: map[string]string{},
		packages:     map[string]*types.Package{},
	}
	//delete(pkgs, "C") // don't import CGo pseudo-package, since it doesn't exist as source code

	/*if hg, ok := pkgs["golang.org/x/net/http/httpguts"]; ok {
		fmt.Println(hg)
	} else {
		log.Fatal("httpguts not found")
	}*/

	for pkgPath, pkg := range fetched.Packages {
		if len(pkg) == 0 {
			log.Fatal("no files in package ", pkgPath)
		}
		imp.packageNames[pkgPath] = pkg[0].Name.Name
	}

	// Initiate type-checking and binding generation
	// for all packages with public APIs.
	for pkgPath := range fetched.Packages {
		if pkgPath == "builtin" {
			// builtin isn't really a package
			continue
		}
		if strings.HasPrefix(pkgPath, "cmd/") {
			// ignore go cmd/ packages
			continue
		}
		if slices.Contains(strings.Split(pkgPath, "/"), "internal") {
			// ignore internal packages
			continue
		}
		_, err = imp.Import(pkgPath)
		if err != nil {
			log.Fatal(err)
		}
	}

	packagePathToImportName := func(path string) string {
		// TODO: Remove this
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
	goBuildLine := fmt.Sprintf("//go:build %v\n", strings.Join(buildTags, " && "))

	{
		var out bytes.Buffer
		out.WriteString(codeGeneratedLine)
		out.WriteString(goBuildLine)
		out.WriteString("package main\n\n")
		out.Write(imp.cs.Code())
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
		out.WriteString(goBuildLine)
		out.WriteString("package main\n\n")
		writeImports(&out, slices.Sorted(maps.Keys(bindingFuncImports)))
		out.WriteString(builtinsCommonCode)
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
					bindingKey, builtin := fn.builtin(imp.cs.ImportNameQualifier)
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
