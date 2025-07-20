package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"log"
	"maps"
	"os"
	"regexp"
	"runtime/pprof"
	"slices"
	"strings"
	"time"

	"github.com/refaktor/ryegen/v2/config"
	"github.com/refaktor/ryegen/v2/converter"
	"github.com/refaktor/ryegen/v2/loader"
	"golang.org/x/tools/go/packages"
)

func isEnvTrue(name string) bool {
	return slices.Contains(
		[]string{"1", "true", "yes"},
		strings.ToLower(os.Getenv(name)),
	)
}

func handleEnvProfile() (stop func()) {
	if !isEnvTrue("RYEGEN_PROFILE") {
		return func() {}
	}

	const path = "ryegen_cpu.prof"
	fmt.Println("Ryegen: profiling enabled, writing to", path)
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	//runtime.SetCPUProfileRate(500)
	if err := pprof.StartCPUProfile(f); err != nil {
		log.Fatal(err)
	}
	return func() {
		fmt.Println("Ryegen: profile saved to", path)
		pprof.StopCPUProfile()
		f.Close()
	}
}

func handleEnvConvGraph(cs *converter.ConverterSet) (stop func()) {
	reStr := os.Getenv("RYEGEN_CONV_GRAPH")
	if reStr == "" {
		return func() {}
	}

	const path = "ryegen_conv_graph.gv"
	fmt.Println("Ryegen: converter dependency graph enabled, will write to", path)
	return func() {
		re, err := regexp.Compile(reStr)
		if err != nil {
			log.Fatal("Converter dependency selection regex:", err)
		}
		fmt.Println("Ryegen: writing converter dependency graph to", path)
		code := cs.DebugDOTCode(re)
		if err := os.WriteFile(path, code, 0666); err != nil {
			log.Fatal(err)
		}
	}
}

func handlePrintTime() (stop func()) {
	now := time.Now()
	return func() {
		fmt.Println("Ryegen: took", time.Since(now))
	}
}

func boolToBinStr(b bool) string {
	if b {
		return "1"
	} else {
		return "0"
	}
}

func main() {
	defer handleEnvProfile()()
	defer handlePrintTime()()

	cfg, err := config.Load("ryegen.toml")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("ryegen.toml not found")
		} else {
			fmt.Println(err)
		}
		os.Exit(1)
	}

	loaderCfg := &loader.Config{}

	for _, src := range cfg.Sources {
		loaderCfg.PackagePatterns = append(loaderCfg.PackagePatterns,
			src.Packages...)
	}

	target := cfg.Targets[0] // TODO: TEMP
	loaderCfg.Env = append(loaderCfg.Env,
		"GOOS="+target.GOOS,
		"GOARCH="+target.GOARCH,
		"CGO_ENABLED="+boolToBinStr(target.CGoEnabled),
	)
	loaderCfg.BuildFlags = append(loaderCfg.BuildFlags,
		"-tags="+target.Tags,
	)

	pkgs, err := loader.Load(loaderCfg)
	if err != nil {
		fmt.Println("loading packages:", err)
		os.Exit(1)
	}

	timeStartGenBindings := time.Now()

	cs := converter.NewConverterSet("main")
	defer handleEnvConvGraph(cs)()

	var bindings []binding

	shouldVisitPackage := func(p *packages.Package) bool {
		for elem := range strings.SplitSeq(p.PkgPath, "/") {
			if elem == "internal" || elem == "cmd" {
				return false
			}
		}
		if strings.HasPrefix(p.PkgPath, "vendor/") {
			// Ignore Go vendored std library modules.
			// See https://cs.opensource.google/go/go/+/master:src/README.vendor.
			// TODO: Figure out if this could break
			// user-vendored modules.
			return false
		}
		return true
	}
	packages.Visit(pkgs, shouldVisitPackage, func(p *packages.Package) {
		if !shouldVisitPackage(p) {
			return
		}

		info := p.TypesInfo
		namedTypes := map[string]*types.Named{}
		for _, f := range p.Syntax {
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
					bindings = append(bindings, newFuncBinding(f, cs.ImportNameQualifier))
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
								if iface, ok := namedTyp.Underlying().(*types.Interface); ok {
									if !iface.IsMethodSet() {
										// Contains type constraints
										continue
									}
								}
								bindings = append(bindings, newMethodBindings(namedTyp, cs.ImportNameQualifier)...)
								namedTypes[spec.Name.Name] = info.ObjectOf(spec.Name).Type().(*types.Named)
							}
						}
					}
				}
			}
		}

		for _, typName := range slices.Sorted(maps.Keys(namedTypes)) {
			typ := namedTypes[typName]

			bindings = append(bindings, newConstructorBinding(typ, cs.ImportNameQualifier))
			bindings = append(bindings, newGetterBindings(typ, cs.ImportNameQualifier)...)
			bindings = append(bindings, newSetterBindings(typ, cs.ImportNameQualifier)...)
		}
	})

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
	var goBuildLine string
	{
		goBuildLine = "//go:build " + target.GOOS + " && " + target.GOARCH
		if target.CGoEnabled {
			goBuildLine += " && cgo"
		}
		goBuildLine += "\n"
	}

	var code []byte
	{
		packageToBindingFuncs := map[string]map[string]binding{}   // package to func name to bindingFunc
		packageToBindingConvName := map[string]map[string]string{} // package to func name to conv name
		for _, fn := range bindings {
			if fn.pkg != nil {
				pkg := fn.pkg.Path()
				convName := cs.Add(fn.requiredConverter, converter.ToRye, pkg+"::"+fn.key())
				if packageToBindingFuncs[pkg] == nil {
					packageToBindingFuncs[pkg] = map[string]binding{}
					packageToBindingConvName[pkg] = map[string]string{}
				}
				packageToBindingFuncs[pkg][fn.key()] = fn
				packageToBindingConvName[pkg][fn.key()] = convName
			}
		}

		var convErr *converter.ConverterError
		code, err = cs.Code()
		if err != nil {
			if errors.As(err, &convErr) {
				fmt.Print(convErr.String())
			} else {
				log.Fatal(err)
			}
		}

		bindingFuncImports := map[string]struct{}{}
		for _, fn := range bindings {
			var pkg string
			if fn.pkg != nil {
				pkg = fn.pkg.Path()
			}
			if !convErr.IsUsable(fn.requiredConverter, converter.ToRye) {
				delete(packageToBindingFuncs[pkg], fn.key())
				delete(packageToBindingConvName[pkg], fn.key())
				continue
			}
			for _, imp := range fn.requiredImports {
				bindingFuncImports[imp.Path()] = struct{}{}
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
					convName := packageToBindingConvName[pkg][bf]
					if fn.exclude {
						continue
					}
					fmt.Fprintf(&out, "\t"+`m["%v"] = %v`+"\n", fn.key(), fn.binding(convName))
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
	{
		var out bytes.Buffer
		out.WriteString(codeGeneratedLine)
		out.WriteString(goBuildLine)
		out.WriteString("package main\n\n")
		out.Write(code)
		if err := os.WriteFile("ryegen_convs.go", out.Bytes(), 0666); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("Ryegen: binding generation took", time.Since(timeStartGenBindings))
}
