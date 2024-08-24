package ryegen

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/module"

	"github.com/BurntSushi/toml"
	"github.com/iancoleman/strcase"
	"github.com/refaktor/ryegen/binder"
	"github.com/refaktor/ryegen/binder/binderctx"
	"github.com/refaktor/ryegen/binder/binderio"
	"github.com/refaktor/ryegen/config"
	"github.com/refaktor/ryegen/ir"
	"github.com/refaktor/ryegen/parser"
	"github.com/refaktor/ryegen/repo"
)

var fset = token.NewFileSet()

// Order of importance (descending):
// - Part of stdlib
// - Prefix of preferPkg
// - Shorter path
// - Smaller string according to strings.Compare
func makeCompareModulePaths(preferPkg string) func(a, b string) int {
	return func(a, b string) int {
		{
			aSp := strings.SplitN(a, "/", 2)
			bSp := strings.SplitN(b, "/", 2)
			if len(aSp) > 0 && len(bSp) > 0 {
				aStd := !strings.Contains(aSp[0], ".")
				bStd := !strings.Contains(bSp[0], ".")
				if aStd && !bStd {
					return -1
				} else if !aStd && bStd {
					return 1
				}
			}
		}
		if preferPkg != "" {
			aPfx := strings.HasPrefix(a, preferPkg)
			bPfx := strings.HasPrefix(b, preferPkg)
			if aPfx && !bPfx {
				return -1
			} else if !aPfx && bPfx {
				return 1
			}
		}
		if len(a) < len(b) {
			return -1
		} else if len(a) > len(b) {
			return 1
		}
		return strings.Compare(a, b)
	}
}

func SortedMapKeys[K cmp.Ordered, V any](m map[K]V) []K {
	res := make([]K, 0, len(m))
	for k := range m {
		res = append(res, k)
	}
	slices.Sort(res)
	return res
}

func Run() {
	configPath := "config.toml"
	if _, err := os.Stat(configPath); err != nil {
		if err := os.WriteFile(configPath, []byte(config.DefaultConfig), 0666); err != nil {
			fmt.Println("create default config:", err)
			os.Exit(1)
		}
		fmt.Println("created default config at", configPath)
		os.Exit(0)
	}
	var cfg config.Config
	if _, err := toml.DecodeFile(configPath, &cfg); err != nil {
		fmt.Println("open config:", err)
		os.Exit(1)
	}

	dstPath := "_srcrepos"

	getRepo := func(pkg, version string) (string, error) {
		have, dir, _, err := repo.Have(dstPath, pkg, version)
		if err != nil {
			return "", err
		}
		if !have {
			log.Printf("downloading %v %v\n", pkg, version)
			_, err := repo.Get(dstPath, pkg, version)
			if err != nil {
				return "", err
			}
		}
		return dir, nil
	}

	srcDir, err := getRepo(cfg.Package, cfg.Version)
	if err != nil {
		fmt.Println("get repo:", err)
		os.Exit(1)
	}

	moduleDirPaths := make(map[string]string)
	moduleDefaultNames := make(map[string]string) // module path to name
	{
		addPkgNames := func(dir, modulePath string) (string, []module.Version, error) {
			goVer, pkgNms, req, err := parser.ParseDirModules(fset, dir, modulePath)
			if err != nil {
				return "", nil, err
			}
			for mod, name := range pkgNms {
				moduleDefaultNames[mod] = name
				moduleDirPaths[mod] = filepath.Join(dir, strings.TrimPrefix(mod, modulePath))
			}
			return goVer, req, nil
		}
		goVer, req, err := addPkgNames(srcDir, cfg.Package)
		if err != nil {
			fmt.Println("parse modules:", err)
			os.Exit(1)
		}
		req = append(req, module.Version{Path: "std", Version: goVer})
		for _, v := range req {
			dir, err := getRepo(v.Path, v.Version)
			if err != nil {
				fmt.Println("get repo:", err)
				os.Exit(1)
			}
			if _, _, err := addPkgNames(dir, v.Path); err != nil {
				fmt.Println("parse modules:", err)
				os.Exit(1)
			}
		}
	}
	modNames := make(ir.UniqueModuleNames) // (underlying: map[string]string) module path to globally unique name
	modNames["C"] = "C"
	{
		moduleNameKeys := make([]string, 0, len(moduleDefaultNames))
		for k := range moduleDefaultNames {
			moduleNameKeys = append(moduleNameKeys, k)
		}
		slices.SortFunc(moduleNameKeys, makeCompareModulePaths(cfg.Package))

		moduleNameIdxs := make(map[string]int) // module name to number of occurrences
		for _, mod := range moduleNameKeys {
			name := moduleDefaultNames[mod]
			impName := name
			if idx := moduleNameIdxs[name]; idx > 0 {
				impName += "_" + strconv.Itoa(idx)
			}
			modNames[mod] = impName
			moduleNameIdxs[name]++
		}
	}

	startTime := time.Now()

	parsedPkgs := make(map[string]struct{})
	genBindingPkgs := make(map[string]struct{}) // mod paths
	irData := ir.New()
	ctx := binderctx.New(&cfg, irData, modNames)

	parseDir := func(dirPath string, modulePath string, genBinding, typeDeclsOnly bool) {
		pkgs, err := parser.ParseDir(fset, dirPath, modulePath)
		if err != nil {
			fmt.Println("parse source:", err)
			os.Exit(1)
		}

		for _, pkg := range pkgs {
			for name, f := range pkg.Files {
				name = strings.TrimPrefix(name, dstPath+string(filepath.Separator))
				tdo := typeDeclsOnly
				if ir.ModulePathIsInternal(ctx.ModNames, pkg.Path) {
					tdo = true
				}
				if err := irData.AddFile(ctx.ModNames, f, name, pkg.Path, moduleDefaultNames, tdo); err != nil {
					fmt.Printf("AddFile: %v: %v\n", pkg.Name, err)
				}
			}
			if genBinding {
				genBindingPkgs[pkg.Path] = struct{}{}
			}
			parsedPkgs[pkg.Path] = struct{}{}
		}
	}

	parseDir(srcDir, cfg.Package, true, false)

	for mod := range irData.RequiredPkgs {
		if _, ok := parsedPkgs[mod]; ok {
			continue
		}
		parseDir(moduleDirPaths[mod], mod, false, true)
	}

	for _, mod := range cfg.IncludeStdLibs {
		if _, ok := parsedPkgs[mod]; ok {
			continue
		}
		parseDir(moduleDirPaths[mod], mod, true, false)
	}

	if err := irData.ResolveInheritancesAndMethods(ctx.ModNames); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	bindingFuncs := make(map[string]*binder.BindingFunc)

	for _, iface := range irData.Interfaces {
		if iface.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, iface.Name) {
			continue
		}
		if _, ok := genBindingPkgs[iface.Name.File.ModulePath]; !ok {
			continue
		}
		for _, fn := range iface.Funcs {
			bind, err := binder.GenerateBinding(ctx, fn)
			if err != nil {
				fmt.Println(fn.String()+":", err)
				continue
			}
			bindingFuncs[bind.FullName()] = bind
		}
	}

	for _, fn := range irData.Funcs {
		if ir.IdentIsInternal(ctx.ModNames, fn.Name) || (fn.Recv != nil && ir.IdentIsInternal(ctx.ModNames, *fn.Recv)) {
			continue
		}
		if _, ok := genBindingPkgs[fn.File.ModulePath]; !ok {
			continue
		}
		bind, err := binder.GenerateBinding(ctx, fn)
		if err != nil {
			fmt.Println(fn.String()+":", err)
			continue
		}
		bindingFuncs[bind.FullName()] = bind
	}

	for _, struc := range irData.Structs {
		if struc.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, struc.Name) {
			continue
		}
		if _, ok := genBindingPkgs[struc.Name.File.ModulePath]; !ok {
			continue
		}
		for _, f := range struc.Fields {
			for _, setter := range []bool{false, true} {
				bind, err := binder.GenerateGetterOrSetter(ctx, f, struc.Name, setter)
				if err != nil {
					s := struc.Name.RyeName + "//" + f.Name.RyeName
					if setter {
						s += "!"
					} else {
						s += "?"
					}
					fmt.Println(s+":", err)
					continue
				}
				bindingFuncs[bind.FullName()] = bind
			}
		}
	}

	for _, value := range irData.Values {
		if value.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, value.Name) {
			continue
		}
		if _, ok := genBindingPkgs[value.Name.File.ModulePath]; !ok {
			continue
		}
		bind, err := binder.GenerateValue(ctx, value)
		if err != nil {
			s := value.Name.RyeName
			fmt.Println(s+":", err)
			continue
		}
		bindingFuncs[bind.FullName()] = bind
	}

	for _, struc := range irData.Structs {
		if struc.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, struc.Name) {
			continue
		}
		if _, ok := genBindingPkgs[struc.Name.File.ModulePath]; !ok {
			continue
		}
		bind, err := binder.GenerateNewStruct(ctx, struc.Name)
		if err != nil {
			s := struc.Name.RyeName
			fmt.Println(s+":", err)
			continue
		}
		if _, ok := bindingFuncs[bind.FullName()]; !ok {
			// Only generate NewMyStruct if the function doesn't already exist.
			bindingFuncs[bind.FullName()] = bind
		}
	}

	requiredIfaceImpls := make(map[string]string)
	for {
		// Generate interface impls recursively until all are implemented,
		// since generating one might cause another one to be required
		addedImpl := false
		for name, iface := range irData.RequiredIfaces {
			ifaceImpl, err := binder.GenerateGenericInterfaceImpl(ctx, iface)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if _, ok := requiredIfaceImpls[name]; !ok {
				addedImpl = true
				requiredIfaceImpls[name] = ifaceImpl
			}
		}
		if !addedImpl {
			break
		}
	}

	bindingListPath := "bindings.txt"
	var bindingList *config.BindingList
	if _, err := os.Stat(bindingListPath); err == nil {
		var err error
		bindingList, err = config.LoadBindingListFromFile(bindingListPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	} else {
		bindingList = config.NewBindingList()
	}
	{
		bindingFuncsToDocstrs := make(map[string]string, len(bindingFuncs))
		for name, binding := range bindingFuncs {
			bindingFuncsToDocstrs[name] = binding.Doc
		}
		if err := bindingList.SaveToFile(bindingListPath, bindingFuncsToDocstrs); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	// rye ident to list of modules with priority
	bindingFuncPrios := make(map[string][]struct {
		Mod  string
		Prio int // less => higher prio
	})
	addBindingFuncPrio := func(bind *binder.BindingFunc) {
		if bind.NameIdent == nil || bind.Recv != "" {
			return
		}

		name, file := bind.SplitGoNameAndMod()
		if ctx.Config.CutNew {
			name = strcase.ToKebab(strings.TrimPrefix(name, "New"))
			if name == "" {
				name = strcase.ToKebab(ctx.ModNames[file.ModulePath])
			}
		}

		ps := bindingFuncPrios[name]
		for _, p := range ps {
			if file.ModulePath == p.Mod {
				return
			}
		}

		prio := math.MaxInt
		for i, v := range ctx.Config.NoPrefix {
			if v == file.ModulePath {
				prio = i
			}
		}
		if prio == math.MaxInt {
			return
		}

		bindingFuncPrios[name] = append(bindingFuncPrios[name], struct {
			Mod  string
			Prio int
		}{
			Mod:  file.ModulePath,
			Prio: prio,
		})
	}
	for _, bind := range bindingFuncs {
		if bind.Recv != "" {
			continue
		}
		addBindingFuncPrio(bind)
	}
	for _, bind := range bindingFuncs {
		if bind.NameIdent == nil {
			continue
		}

		newName, file := bind.SplitGoNameAndMod()

		newNameIsPfx := false
		if ctx.Config.CutNew {
			newName = strings.TrimPrefix(newName, "New")
			if newName == "" {
				newName = strcase.ToKebab(ctx.ModNames[file.ModulePath])
				newNameIsPfx = true
			}
		}
		newName = strcase.ToKebab(newName)

		if bind.Recv == "" {
			prios := bindingFuncPrios[newName]
			isHighestPrio := len(prios) > 0 && slices.MinFunc(prios, func(a, b struct {
				Mod  string
				Prio int
			}) int {
				return a.Prio - b.Prio
			}).Mod == file.ModulePath
			if !(isHighestPrio || (newNameIsPfx && len(prios) == 0)) {
				moduleName := ctx.ModNames[file.ModulePath]
				for _, pfx := range ctx.Config.CustomPrefixes {
					name := pfx[0]
					path := pfx[1]
					if path == file.ModulePath {
						moduleName = name
					}
				}
				newName = strcase.ToKebab(moduleName) + "-" + newName
			}
		}

		bind.Name = newName
	}

	ctx.UsedImports["github.com/refaktor/rye/env"] = struct{}{}
	ctx.UsedImports["github.com/refaktor/rye/evaldo"] = struct{}{}

	rootModuleName := ctx.ModNames[cfg.Package]
	buildFlag := strings.Replace(cfg.BuildFlag, "*", rootModuleName, 1)

	outDir := filepath.Join(cfg.OutDir, rootModuleName)
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	outFileNot := filepath.Join(outDir, "builtins.not.go")
	outFile := filepath.Join(outDir, "builtins.go")

	var cb binderio.CodeBuilder

	cb.Linef(`// Code generated by ryegen. DO NOT EDIT.`)
	cb.Linef(``)
	cb.Linef(`//go:build !%v`, buildFlag)
	cb.Linef(``)
	cb.Linef(`package %v`, rootModuleName)
	cb.Linef(``)
	cb.Linef(`import "github.com/refaktor/rye/env"`)
	cb.Linef(``)
	cb.Linef(`var Builtins = map[string]*env.Builtin{}`)

	if fmtErr, err := cb.SaveToFile(outFileNot); err != nil || fmtErr != nil {
		fmt.Println("save binding dummy:", "general:", err, "; fmt:", fmtErr)
		os.Exit(1)
	}
	cb.Reset()

	cb.Linef(`// Code generated by ryegen. DO NOT EDIT.`)
	cb.Linef(``)
	cb.Linef(`//go:build %v`, buildFlag)
	cb.Linef(``)
	cb.Linef(`package %v`, rootModuleName)
	cb.Linef(``)
	cb.Linef(`import (`)
	cb.Indent++
	for _, mod := range SortedMapKeys(ctx.UsedImports) {
		defaultName := moduleDefaultNames[mod]
		uniqueName := ctx.ModNames[mod]
		if defaultName == uniqueName {
			cb.Linef(`"%v"`, mod)
		} else {
			cb.Linef(`%v "%v"`, uniqueName, mod)
		}
	}
	cb.Indent--
	cb.Linef(`)`)
	cb.Linef(``)

	cb.Linef(`// Force-use evaldo and env packages since tracking them would be too complicated`)
	cb.Linef(`var _ = evaldo.BuiltinNames`)
	cb.Linef(`var _ = env.Object(nil)`)
	cb.Linef(``)

	cb.Linef(`func boolToInt64(x bool) int64 {`)
	cb.Indent++
	cb.Linef(`var res int64`)
	cb.Linef(`if x {`)
	cb.Indent++
	cb.Linef(`res = 1`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`return res`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	cb.Linef(`var ryeStructNameLookup = map[string]string{`)
	cb.Indent++
	{
		typNames := make(map[string]string, len(irData.Structs)*2)
		for _, struc := range irData.Structs {
			id := struc.Name
			if !ir.IdentExprIsExported(id.Expr) || ir.IdentIsInternal(ctx.ModNames, id) {
				continue
			}
			var nameNoMod string
			switch expr := id.Expr.(type) {
			case *ast.Ident:
				nameNoMod = expr.Name
			case *ast.StarExpr:
				id, ok := expr.X.(*ast.Ident)
				if !ok {
					continue
				}
				nameNoMod = "*" + id.Name
			case *ast.SelectorExpr:
				nameNoMod = expr.Sel.Name
			default:
				continue
			}
			typNames[id.File.ModulePath+".*"+nameNoMod] = "ptr-" + id.RyeName
		}
		for _, k := range SortedMapKeys(typNames) {
			cb.Linef(`"%v": "%v",`, k, typNames[k])
		}
	}
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	for _, key := range SortedMapKeys(irData.RequiredIfaces) {
		rep := strings.NewReplacer(`((RYEGEN:FUNCNAME))`, "context to "+key)
		cb.Append(rep.Replace(requiredIfaceImpls[key]))
	}

	cb.Linef(`var Builtins = map[string]*env.Builtin{`)
	cb.Indent++

	cb.Linef(`"nil": {`)
	cb.Indent++
	cb.Linef(`Doc: "nil value for go types",`)
	cb.Linef(`Fn: func(ps *env.ProgramState, arg0, arg1, arg2, arg3, arg4 env.Object) env.Object {`)
	cb.Indent++
	cb.Linef(`return *env.NewInteger(0)`)
	cb.Indent--
	cb.Linef(`},`)
	cb.Indent--
	cb.Linef(`},`)

	numWrittenBindings := 0
	for _, k := range SortedMapKeys(bindingFuncs) {
		if enabled, ok := bindingList.Enabled[k]; ok && !enabled {
			continue
		}
		bind := bindingFuncs[k]
		cb.Linef(`"%v": {`, bind.FullName())
		cb.Indent++
		cb.Linef(`Doc: "%v",`, bind.Doc)
		cb.Linef(`Argsn: %v,`, bind.Argsn)
		cb.Linef(`Fn: func(ps *env.ProgramState, arg0, arg1, arg2, arg3, arg4 env.Object) env.Object {`)
		cb.Indent++
		rep := strings.NewReplacer(`((RYEGEN:FUNCNAME))`, bind.FullName())
		cb.Append(rep.Replace(bind.Body))
		cb.Indent--
		cb.Linef(`},`)
		cb.Indent--
		cb.Linef(`},`)
		numWrittenBindings++
	}

	cb.Indent--
	cb.Linef(`}`)

	log.Printf("Generated bindings containing %v/%v functions in %v", numWrittenBindings, len(bindingFuncs), time.Since(startTime))
	{
		fmtErr, err := cb.SaveToFile(outFile)
		if err != nil {
			fmt.Println("save bindings:", err)
			os.Exit(1)
		}
		if fmtErr != nil {
			fmt.Println("cannot format bindings:", fmtErr)
			fmt.Println("Saved as unformatted go code instead.")
		}
	}
	log.Printf("Wrote bindings to %v", outFile)
}
