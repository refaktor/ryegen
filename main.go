package ryegen

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"iter"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/mod/module"

	"github.com/iancoleman/strcase"
	"github.com/olekukonko/tablewriter"
	"github.com/refaktor/ryegen/binder"
	"github.com/refaktor/ryegen/binder/binderio"
	"github.com/refaktor/ryegen/config"
	"github.com/refaktor/ryegen/ir"
	"github.com/refaktor/ryegen/parser"
	"github.com/refaktor/ryegen/repo"
)

// modulePathElementVersion parses strings like "v2", "v3" etc.
func modulePathElementVersion(s string) int {
	if strings.HasPrefix(s, "v") {
		ver, err := strconv.Atoi(s[1:])
		if err == nil && ver >= 1 {
			return ver
		}
	}
	return -1
}

// removeModulePathVersionElements removes all "v2", "v3" etc. parts.
func removeModulePathVersionElements(s string) string {
	sp := strings.Split(s, "/")
	spOut := []string{}
	for _, v := range sp {
		if modulePathElementVersion(v) == -1 {
			spOut = append(spOut, v)
		}
	}
	return strings.Join(spOut, "/")
}

// Order of importance (descending):
// - Part of stdlib
// - Prefix of preferPkg
// - Shorter path (ignoring version numbers)
// - Smaller string according to strings.Compare (ignoring version numbers)
// - Larger version number (e.g. v2, v3)
func makeCompareModulePaths(preferPkg string) func(a, b string) int {
	return func(a, b string) int {
		aOrig, bOrig := a, b
		a, b = removeModulePathVersionElements(a), removeModulePathVersionElements(b)
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
			aPfx := strings.HasPrefix(aOrig, preferPkg)
			bPfx := strings.HasPrefix(bOrig, preferPkg)
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
		if a > b {
			return -1
		} else if a < b {
			return 1
		}
		{
			aSp := strings.Split(aOrig, "/")
			bSp := strings.Split(bOrig, "/")
			if len(aSp) >= 1 && len(bSp) >= 1 {
				if len(aSp) == len(bSp) &&
					modulePathElementVersion(aSp[len(aSp)-1]) > modulePathElementVersion(bSp[len(bSp)-1]) {
					return -1
				}
				if len(aSp) == len(bSp)+1 &&
					modulePathElementVersion(aSp[len(aSp)-1]) > 1 {
					return -1
				}
				if len(aSp) == len(bSp) &&
					modulePathElementVersion(aSp[len(aSp)-1]) < modulePathElementVersion(bSp[len(bSp)-1]) {
					return 1
				}
				if len(aSp)+1 == len(bSp) &&
					modulePathElementVersion(bSp[len(bSp)-1]) > 1 {
					return 1
				}
			}
		}
		return strings.Compare(aOrig, bOrig)
	}
}

func sortedMapAll[Map ~map[K]V, K cmp.Ordered, V any](m Map) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		ks := make([]K, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		slices.Sort(ks)
		for _, k := range ks {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}

func recursivelyGetRepo(
	dstPath, pkg, ver string,
) (
	// module path to unique (short) module name
	modUniqueNames ir.UniqueModuleNames,
	// module path to directory path
	modDirPaths map[string]string,
	// module path to name (declared in "package <name>" line)
	modDefaultNames map[string]string,
	err error,
) {
	modUniqueNames = make(ir.UniqueModuleNames)
	modDirPaths = make(map[string]string)
	modDefaultNames = make(map[string]string)

	getRepo := func(pkg, version string) (string, error) {
		have, dir, _, err := repo.Have(dstPath, pkg, version)
		if err != nil {
			return "", err
		}
		if !have {
			fmt.Printf("downloading %v %v\n", pkg, version)
			_, err := repo.Get(dstPath, pkg, version)
			if err != nil {
				return "", err
			}
		}
		return dir, nil
	}

	srcDir, err := getRepo(pkg, ver)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get repo: %w", err)
	}

	{
		addPkgNames := func(dir, modulePath string) (string, []module.Version, error) {
			goVer, pkgNms, req, err := parser.ParseDirModules(token.NewFileSet(), dir, modulePath)
			if err != nil {
				return "", nil, err
			}
			for mod, name := range pkgNms {
				if name != "" {
					modDefaultNames[mod] = name
				}
				modDirPaths[mod] = filepath.Join(dir, strings.TrimPrefix(mod, modulePath))
			}
			return goVer, req, nil
		}
		goVer, req, err := addPkgNames(srcDir, pkg)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse modules: %w", err)
		}
		req = append(req, module.Version{Path: "std", Version: goVer})
		for _, v := range req {
			dir, err := getRepo(v.Path, v.Version)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("get repo: %w", err)
			}
			if _, _, err := addPkgNames(dir, v.Path); err != nil {
				return nil, nil, nil, fmt.Errorf("parse modules: %w", err)
			}
		}
	}
	modUniqueNames["C"] = "C"
	{
		moduleNameKeys := make([]string, 0, len(modDefaultNames))
		for k := range modDefaultNames {
			moduleNameKeys = append(moduleNameKeys, k)
		}
		slices.SortFunc(moduleNameKeys, makeCompareModulePaths(pkg))

		existingModuleNames := make(map[string]struct{})
		for _, modPath := range moduleNameKeys {
			// Create a unique module path. If the default name as declared in the
			// "package <name>" directive doesn't work, try prepending the previous
			// element of the path.
			// Does not repeat name components, or include version numbers like "v2".
			// Example:
			// 	modPath = github.com/username/reponame/resources/audio
			//  "audio" is already taken.
			//  Try "resources_audio".
			//  If that's already taken, try "reponame_resources_audio" etc.

			modPathElems := strings.Split(removeModulePathVersionElements(modPath), "/")
			nameComponents := []string{modDefaultNames[modPath]}
			for ; func() bool {
				_, exists := existingModuleNames[strings.Join(nameComponents, "_")]
				return exists
			}(); modPathElems = modPathElems[:len(modPathElems)-1] {
				if len(modPathElems) == 0 {
					return nil, nil, nil, fmt.Errorf("cannot create unique module name for", modPath)
				}

				lastElem := modPathElems[len(modPathElems)-1]
				lastElemSnakeCase := strcase.ToSnake(lastElem)
				if slices.Contains(nameComponents, lastElemSnakeCase) {
					continue
				}

				nameComponents = append([]string{lastElemSnakeCase}, nameComponents...)
			}
			name := strings.Join(nameComponents, "_")
			modUniqueNames[modPath] = name
			existingModuleNames[name] = struct{}{}
		}
	}

	return
}

func parsePkgs(
	pkgDlPath string,
	pkgs []string,
	modUniqueNames ir.UniqueModuleNames,
	modDirPaths map[string]string,
	modDefaultNames map[string]string,
) (
	irData *ir.IR,
	genBindingsForPkgs []string,
	err error,
) {
	var fileInfo []ir.IRInputFileInfo
	genBindPkgs := make(map[string]struct{}) // mod paths

	parseDirGo := func(dirPath string, modulePath string) error {
		pkgs, err := parser.ParseDir(token.NewFileSet(), dirPath, modulePath, -1)
		if err != nil {
			return err
		}

		for _, pkg := range pkgs {
			for name, f := range pkg.Files {
				name := strings.TrimPrefix(name, pkgDlPath+string(filepath.Separator))
				fileInfo = append(fileInfo, ir.IRInputFileInfo{
					File:       f,
					Name:       name,
					ModulePath: pkg.Path,
				})
			}
			genBindPkgs[pkg.Path] = struct{}{}
		}
		return nil
	}

	slices.SortFunc(fileInfo, func(a ir.IRInputFileInfo, b ir.IRInputFileInfo) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, pkg := range pkgs {
		dirPath, ok := modDirPaths[pkg]
		if !ok {
			return nil, nil, fmt.Errorf("unknown package: %v", pkg)
		}
		if err := parseDirGo(dirPath, pkg); err != nil {
			return nil, nil, err
		}
	}

	irData, err = ir.Parse(
		modUniqueNames,
		modDefaultNames,
		fileInfo,
		func(modulePath string) (map[string]*ast.File, error) {
			dirPath, ok := modDirPaths[modulePath]
			if !ok {
				return nil, fmt.Errorf("unknown package: %v", modulePath)
			}
			pkgs, err := parser.ParseDir(token.NewFileSet(), dirPath, modulePath, 1)
			if err != nil {
				return nil, err
			}

			res := make(map[string]*ast.File)
			for _, pkg := range pkgs {
				for name, f := range pkg.Files {
					name := strings.TrimPrefix(name, pkgDlPath+string(filepath.Separator))
					if _, ok := res[name]; ok {
						return nil, fmt.Errorf("getDependency: duplicate file name %v in package %v", name, pkg.Name)
					}
					res[name] = f
				}
			}
			return res, nil
		},
	)
	if err != nil {
		return nil, nil, err
	}

	return irData, slices.Sorted(maps.Keys(genBindPkgs)), nil
}

func genBindings(
	targetPkgs []string,
	ctx *binder.Context,
) (
	bindings []*binder.BindingFunc,
	genericInterfaceImpls []string,
	deps *binder.Dependencies,
	err error,
) {
	deps = binder.NewDependencies()

	for _, iface := range sortedMapAll(ctx.IR.Interfaces) {
		if iface.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, iface.Name) {
			continue
		}
		if !slices.Contains(targetPkgs, iface.Name.File.ModulePath) {
			continue
		}
		for _, fn := range iface.Funcs {
			bind, err := binder.GenerateBinding(deps, ctx, fn)
			if err != nil {
				fmt.Println(fn.String()+":", err)
				continue
			}
			bindings = append(bindings, bind)
		}
	}

	for _, fn := range sortedMapAll(ctx.IR.Funcs) {
		if ir.ModulePathIsInternal(ctx.ModNames, fn.File.ModulePath) || (fn.Recv != nil && ir.IdentIsInternal(ctx.ModNames, *fn.Recv)) {
			continue
		}
		if !slices.Contains(targetPkgs, fn.File.ModulePath) {
			continue
		}
		bind, err := binder.GenerateBinding(deps, ctx, fn)
		if err != nil {
			fmt.Println(fn.String()+":", err)
			continue
		}
		bindings = append(bindings, bind)
	}

	for _, struc := range sortedMapAll(ctx.IR.Structs) {
		if struc.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, struc.Name) {
			continue
		}
		if !slices.Contains(targetPkgs, struc.Name.File.ModulePath) {
			continue
		}
		for _, f := range struc.Fields {
			for _, setter := range []bool{false, true} {
				bind, err := binder.GenerateGetterOrSetter(deps, ctx, f, struc.Name, setter)
				if err != nil {
					s := struc.Name.Name + "//" + f.Name.Name
					if setter {
						s += "!"
					} else {
						s += "?"
					}
					fmt.Println(s+":", err)
					continue
				}
				bindings = append(bindings, bind)
			}
		}
	}

	for _, value := range sortedMapAll(ctx.IR.Values) {
		if value.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, value.Name) {
			continue
		}
		if !slices.Contains(targetPkgs, value.Name.File.ModulePath) {
			continue
		}
		bind, err := binder.GenerateValue(deps, ctx, value)
		if err != nil {
			s := value.Name.Name
			fmt.Println(s+":", err)
			continue
		}
		bindings = append(bindings, bind)
	}

	for _, struc := range sortedMapAll(ctx.IR.Structs) {
		if struc.Name.File == nil || ir.IdentIsInternal(ctx.ModNames, struc.Name) {
			continue
		}
		if !slices.Contains(targetPkgs, struc.Name.File.ModulePath) {
			continue
		}
		bind, err := binder.GenerateNewStruct(deps, ctx, struc.Name)
		if err != nil {
			s := struc.Name.Name
			fmt.Println(s+":", err)
			continue
		}
		if !slices.ContainsFunc(bindings, func(b *binder.BindingFunc) bool {
			return b.UniqueName(ctx) == bind.UniqueName(ctx)
		}) {
			// Only generate NewMyStruct if the function doesn't already exist.
			bindings = append(bindings, bind)
		}
	}

	genericIfaceImpls := make(map[string]string)
	for {
		// Generate interface impls recursively until all are implemented,
		// since generating one might cause another one to be required
		addedImpl := false
		for name, iface := range sortedMapAll(deps.GenericInterfaceImpls) {
			if _, ok := genericIfaceImpls[name]; ok {
				continue
			}
			ifaceImpl, err := binder.GenerateGenericInterfaceImpl(deps, ctx, iface)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("generate generic interface impl: %w", err)
			}
			addedImpl = true
			rep := strings.NewReplacer(`((RYEGEN:FUNCNAME))`, "context to "+iface.Name.Name)
			genericIfaceImpls[name] = rep.Replace(ifaceImpl)
		}
		if !addedImpl {
			break
		}
	}
	genericInterfaceImpls = slices.Collect(maps.Values(genericIfaceImpls))

	return
}

func Run() {
	var cfg *config.Config
	{
		const configPath = "config.toml"
		var createdDefault bool
		var err error
		cfg, createdDefault, err = config.ReadConfigFromFileOrCreateDefault(configPath)
		if err != nil {
			fmt.Println("open config:", err)
			os.Exit(1)
		}
		if createdDefault {
			fmt.Println("created default config at", configPath)
			os.Exit(0)
		}
	}

	const pkgDlPath = "_srcrepos"

	timeStart := time.Now()

	modUniqueNames,
		modDirPaths,
		modDefaultNames,
		err := recursivelyGetRepo(pkgDlPath, cfg.Package, cfg.Version)
	if err != nil {
		fmt.Println("get repo:", err)
		os.Exit(1)
	}

	timeGetRepos := time.Since(timeStart)
	timeStart = time.Now()

	irData, genBindingsForPkgs, err := parsePkgs(
		pkgDlPath,
		append([]string{cfg.Package}, cfg.IncludeStdLibs...),
		modUniqueNames,
		modDirPaths,
		modDefaultNames,
	)
	if err != nil {
		fmt.Println("parse packages:", err)
		os.Exit(1)
	}

	timeParse := time.Since(timeStart)
	timeStart = time.Now()

	ctx := binder.NewContext(cfg, irData, modUniqueNames)

	bindings, genericInterfaceImpls, dependencies, err := genBindings(genBindingsForPkgs, ctx)
	if err != nil {
		fmt.Println("generate bindings:", err)
		os.Exit(1)
	}

	timeGenBindings := time.Since(timeStart)
	timeStart = time.Now()

	const bindingListPath = "bindings.txt"
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
		bindingFuncsToDocstrs := make(map[string]string, len(bindings))
		for _, bind := range bindings {
			bindingFuncsToDocstrs[bind.UniqueName(ctx)] = bind.Doc
		}
		if err := bindingList.SaveToFile(bindingListPath, bindingFuncsToDocstrs); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}

	timeReadWriteBindingsTXT := time.Since(timeStart)
	timeStart = time.Now()

	dependencies.Imports["github.com/refaktor/rye/env"] = struct{}{}
	dependencies.Imports["github.com/refaktor/rye/evaldo"] = struct{}{}
	dependencies.Imports["reflect"] = struct{}{}

	var fullBindingName string
	{
		var b strings.Builder
		for _, r := range cfg.Package {
			r = unicode.ToLower(r)
			if (r < 'a' || r > 'z') &&
				(r < '0' || r > '9') {
				r = '_'
			}
			b.WriteRune(r)
		}
		fullBindingName = b.String()
	}

	outDir := filepath.Join(cfg.OutDir, fullBindingName)
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	outFileCustom := filepath.Join(outDir, "custom.go")
	outFileNot := filepath.Join(outDir, "generated.not.go")
	outFile := filepath.Join(outDir, "generated.go")

	if _, err := os.Stat(outFileCustom); os.IsNotExist(err) {
		var cb binderio.CodeBuilder

		cb.Linef(`// Add your custom builtins to this file.`)
		cb.Linef(``)
		cb.Linef(`package %v`, fullBindingName)
		cb.Linef(``)
		cb.Linef(`import (`)
		cb.Indent++
		cb.Linef(`"strings"`)
		cb.Linef(``)
		cb.Linef(`"github.com/refaktor/rye/env"`)
		cb.Indent--
		cb.Linef(`)`)
		cb.Linef(``)
		cb.Linef(`var builtinsCustom = map[string]*env.Builtin{`)
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
		cb.Linef(`"kind": {`)
		cb.Indent++
		cb.Linef(`Doc: "underlying kind of a go native",`)
		cb.Linef(`Fn: func(ps *env.ProgramState, arg0, arg1, arg2, arg3, arg4 env.Object) env.Object {`)
		cb.Indent++
		cb.Linef(`nat, ok := arg0.(env.Native)`)
		cb.Linef(`if !ok {`)
		cb.Indent++
		cb.Linef(`ps.FailureFlag = true`)
		cb.Linef(`return env.NewError("kind: arg0: expected native")`)
		cb.Indent--
		cb.Linef(`}`)
		cb.Linef(`s := ps.Idx.GetWord(nat.Kind.Index)`)
		cb.Linef(`s = s[3:len(s)-1] // remove surrounding "Go()"`)
		cb.Linef(`s = strings.TrimPrefix(s, "*") // remove potential pointer "*"`)
		cb.Linef(`return *env.NewString(s)`)
		cb.Indent--
		cb.Linef(`},`)
		cb.Indent--
		cb.Linef(`},`)
		cb.Indent--
		cb.Linef(`// Add your custom builtins here:`)
		cb.Linef(`}`)

		if fmtErr, err := cb.SaveToFile(outFileCustom); err != nil || fmtErr != nil {
			fmt.Println("save custom.go:", "general:", err, "; fmt:", fmtErr)
			os.Exit(1)
		}
	} else if err != nil {
		fmt.Println("stat custom.go:", err)
		os.Exit(1)
	}

	if cfg.DontBuildFlag == "" {
		if _, err := os.Stat(outFileNot); err == nil {
			if err := os.Remove(outFileNot); err != nil {
				fmt.Printf("remove %v: %v", outFileNot, err)
				os.Exit(1)
			}
		}
	} else {
		var cb binderio.CodeBuilder

		cb.Linef(`// Code generated by ryegen. DO NOT EDIT.`)
		cb.Linef(``)
		cb.Linef(`//go:build %v`, cfg.DontBuildFlag)
		cb.Linef(``)
		cb.Linef(`package %v`, fullBindingName)
		cb.Linef(``)
		cb.Linef(`import "github.com/refaktor/rye/env"`)
		cb.Linef(``)
		cb.Linef(`var Builtins = map[string]*env.Builtin{}`)

		if fmtErr, err := cb.SaveToFile(outFileNot); err != nil || fmtErr != nil {
			fmt.Println("save binding dummy:", "general:", err, "; fmt:", fmtErr)
			os.Exit(1)
		}
	}

	var cb binderio.CodeBuilder

	cb.Linef(`// Code generated by ryegen. DO NOT EDIT.`)
	cb.Linef(``)
	cb.Linef(`// You can add custom binding code to builtins_custom.go!`)
	cb.Linef(``)
	if cfg.DontBuildFlag != "" {
		cb.Linef(`//go:build !%v`, cfg.DontBuildFlag)
		cb.Linef(``)
	}
	cb.Linef(`package %v`, fullBindingName)
	cb.Linef(``)
	cb.Linef(`import (`)
	cb.Indent++
	for _, mod := range slices.Sorted(maps.Keys(dependencies.Imports)) {
		defaultName := modDefaultNames[mod]
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

	cb.Linef(``)
	cb.Linef(`var Builtins map[string]*env.Builtin`)
	cb.Linef(``)
	cb.Linef(`func init() {`)
	cb.Indent++
	cb.Linef(`Builtins = make(map[string]*env.Builtin, len(builtinsGenerated) + len(builtinsCustom))`)
	cb.Linef(`for k, v := range builtinsGenerated {`)
	cb.Indent++
	cb.Linef(`Builtins[k] = v`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`for k, v := range builtinsCustom {`)
	cb.Indent++
	cb.Linef(`Builtins[k] = v`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Indent--
	cb.Linef(`}`)

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

	cb.Linef(`func objectDebugString(idx *env.Idxs, v any) string {`)
	cb.Indent++
	cb.Linef(`if v, ok := v.(env.Object); ok {`)
	cb.Indent++
	cb.Linef(`return v.Inspect(*idx)`)
	cb.Indent--
	cb.Linef(`} else {`)
	cb.Indent++
	cb.Linef(`return "[Non-object of type "+reflect.TypeOf(v).String()+"]"`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	cb.Linef(`func ifaceToNative(idx *env.Idxs, v any, ifaceName string) env.Native {`)
	cb.Indent++
	cb.Linef(`rV := reflect.ValueOf(v)`)
	cb.Linef(`var typRyeName string`)
	cb.Linef(`var ok bool`)
	cb.Linef(`if rV.Type() != nil {`)
	cb.Indent++
	cb.Linef(`var typPfx string`)
	cb.Linef(`if rV.Type().Kind() == reflect.Struct {`)
	cb.Indent++
	cb.Linef(`newRV := reflect.New(rV.Type())`)
	cb.Linef(`newRV.Elem().Set(rV)`)
	cb.Linef(`rV = newRV`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`typ := rV.Type()`)
	cb.Linef(`if typ.Kind() == reflect.Pointer {`)
	cb.Indent++
	cb.Linef(`typ = rV.Type().Elem()`)
	cb.Linef(`typPfx = "*"`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`typRyeName, ok = ryeStructNameLookup[typ.PkgPath()+"."+typPfx+typ.Name()]`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`if ok {`)
	cb.Indent++
	cb.Linef(`return *env.NewNative(idx, rV.Interface(), typRyeName)`)
	cb.Indent--
	cb.Linef(`} else {`)
	cb.Indent++
	cb.Linef(`return *env.NewNative(idx, rV.Interface(), ifaceName)`)
	cb.Indent--
	cb.Linef(`}`)
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

			var err error
			id, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, id.File, &ast.StarExpr{X: id.Expr})
			if err != nil {
				panic(err)
			}

			typNames[id.File.ModulePath+".*"+nameNoMod] = id.RyeName()
		}
		for k, v := range sortedMapAll(typNames) {
			cb.Linef(`"%v": "%v",`, k, v)
		}
	}
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	for _, ifaceImpl := range slices.Sorted(slices.Values(genericInterfaceImpls)) {
		cb.Append(ifaceImpl)
	}

	cb.Linef(`var builtinsGenerated = map[string]*env.Builtin{`)
	cb.Indent++

	sortedBindings := slices.SortedFunc(slices.Values(bindings), func(bf1, bf2 *binder.BindingFunc) int {
		return strings.Compare(bf1.UniqueName(ctx), bf2.UniqueName(ctx))
	})

	bindingNames := make([]string, len(sortedBindings))
	{
		namePrios := make([]int, len(sortedBindings))
		for i, bind := range sortedBindings {
			prio := slices.Index(cfg.NoPrefix, bind.File.ModulePath)
			if prio == -1 {
				prio = math.MaxInt
			}
			namePrios[i] = prio
		}
		nameCandidates := make([][]string, len(sortedBindings))
		for i, bind := range sortedBindings {
			nameCandidates[i] = bind.RyeifiedNameCandidates(ctx, namePrios[i] != math.MaxInt, cfg.CutNew)
		}
		for {
			foundConflict := false
			topNames := make(map[string]int) // current top candidate to index into sortedBindings
			for i, bind := range sortedBindings {
				if len(nameCandidates[i]) == 0 {
					fmt.Println("unable to resolve naming conflict for", bind.UniqueName(ctx))
					os.Exit(1)
				}
				topName := nameCandidates[i][0]
				if otherI, exists := topNames[topName]; exists {
					if namePrios[otherI] < namePrios[i] /* lower means higher priority (in this case otherI has higher priority) */ {
						nameCandidates[i] = nameCandidates[i][1:]
						topNames[topName] = otherI
						foundConflict = true
					} else if namePrios[i] < namePrios[otherI] /* i has higher priority than otherI */ {
						nameCandidates[otherI] = nameCandidates[otherI][1:]
						topNames[topName] = i
						foundConflict = true
					} else {
						// TODO: Find a better way to do this.
						fmt.Printf(
							"Unable to resolve naming conflict between %v and %v. Renaming %v to %v.\n",
							bind.UniqueName(ctx), sortedBindings[otherI].UniqueName(ctx),
							nameCandidates[i][0], nameCandidates[i][0]+"-1",
						)
						nameCandidates[i][0] += "-1"
						topName = nameCandidates[i][0]
						topNames[topName] = i
						foundConflict = true
					}
				} else {
					topNames[topName] = i
				}
			}
			if !foundConflict {
				// no conflicts left
				break
			}
		}
		for i := range sortedBindings {
			bindingNames[i] = nameCandidates[i][0]
		}
	}

	numWrittenBindings := 0
	numBindingsByCategory := make(map[string]int)
	numWrittenBindingsByCategory := make(map[string]int)
	for i, bind := range sortedBindings {
		numBindingsByCategory[bind.Category]++
		if enabled, ok := bindingList.Enabled[bind.UniqueName(ctx)]; ok && !enabled {
			continue
		}
		cb.Linef(`"%v": {`, bindingNames[i])
		cb.Indent++
		cb.Linef(`Doc: "%v",`, bind.Doc)
		cb.Linef(`Argsn: %v,`, bind.Argsn)
		cb.Linef(`Fn: func(ps *env.ProgramState, arg0, arg1, arg2, arg3, arg4 env.Object) env.Object {`)
		cb.Indent++
		rep := strings.NewReplacer(`((RYEGEN:FUNCNAME))`, bindingNames[i])
		cb.Append(rep.Replace(bind.Body))
		cb.Indent--
		cb.Linef(`},`)
		cb.Indent--
		cb.Linef(`},`)
		numWrittenBindingsByCategory[bind.Category]++
		numWrittenBindings++
	}

	cb.Indent--
	cb.Linef(`}`)

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

	timeWriteCode := time.Since(timeStart)
	timeStart = time.Now()

	fmt.Println()
	fmt.Printf("==Binding stats==\n")
	fmt.Printf("Generated %v generic interface implementations.\n", len(genericInterfaceImpls))
	fmt.Printf("Number of generated builtins (excludes generic interface impls):\n")
	{
		tbl := tablewriter.NewWriter(os.Stdout)
		tbl.SetHeader([]string{"Category", "Written/Total"})
		for _, cat := range slices.Sorted(maps.Keys(numBindingsByCategory)) {
			tbl.Append([]string{cat, fmt.Sprintf("%v/%v", numWrittenBindingsByCategory[cat], numBindingsByCategory[cat])})
		}
		tbl.Append([]string{"==TOTAL==", fmt.Sprintf("%v/%v", numWrittenBindings, len(bindings))})
		tbl.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_CENTER})
		tbl.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
		tbl.SetCenterSeparator("|")
		tbl.Render()
	}
	fmt.Println()
	fmt.Printf("==Timing stats==\n")
	fmt.Printf("Fetched/checked source repos in %v.\n", timeGetRepos)
	fmt.Printf("Binding generation tasks (excludes fetching/checking source repos):\n")
	{
		timeTotal := timeParse + timeGenBindings + timeReadWriteBindingsTXT + timeWriteCode
		timePercent := func(t time.Duration) string {
			return strconv.FormatFloat(
				float64(t)/float64(timeTotal)*100,
				'f', 2, 64,
			)
		}

		tbl := tablewriter.NewWriter(os.Stdout)
		tbl.SetHeader([]string{"Task", "Time", "Time %"})
		tbl.AppendBulk([][]string{
			{"Parse", timeParse.String(), timePercent(timeParse)},
			{"Generate bindings", timeGenBindings.String(), timePercent(timeGenBindings)},
			{"Read/Write bindings.txt", timeReadWriteBindingsTXT.String(), timePercent(timeReadWriteBindingsTXT)},
			{"Write and format code", timeWriteCode.String(), timePercent(timeWriteCode)},
			{"==TOTAL==", timeTotal.String(), "100"},
		})
		tbl.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_RIGHT})
		tbl.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
		tbl.SetCenterSeparator("|")
		tbl.Render()
	}

	fmt.Println()
	fmt.Printf("Wrote bindings to %v", outFile)
}
