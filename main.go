package ryegen

import (
	"errors"
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
	"github.com/refaktor/ryegen/repo"
)

var fset = token.NewFileSet()

func makeMakeRetArgErr(argn int) func(inner string) string {
	return func(inner string) string {
		return fmt.Sprintf(
			`ps.FailureFlag = true
return env.NewError("((RYEGEN:FUNCNAME)): arg %v: %v")
`,
			argn+1,
			inner,
		)
	}
}

type BindingFunc struct {
	Recv      string
	Name      string
	NameIdent *Ident
	Doc       string
	Argsn     int
	Body      string
}

func (bind *BindingFunc) FullName() string {
	if bind.Recv != "" {
		return bind.Recv + "//" + bind.Name
	} else {
		return bind.Name
	}
}

func (bind *BindingFunc) SplitGoNameAndMod() (string, *File) {
	file := bind.NameIdent.File
	var name string
	switch expr := bind.NameIdent.Expr.(type) {
	case *ast.Ident:
		name = expr.Name
	case *ast.SelectorExpr:
		mod, ok := expr.X.(*ast.Ident)
		if !ok {
			panic("expected ast.SelectorExpr.X to be of type *ast.Ident")
		}
		file, ok = file.ImportsByName[mod.Name]
		if !ok {
			panic(fmt.Errorf("module %v imported by %v not found", mod.Name, file.Name))
		}
		name = expr.Sel.Name
	default:
		panic("expected func name identifier to be of type *ast.Ident or *ast.SelectorExpr")
	}
	return name, file
}

func GenerateBinding(ctx *Context, fn *Func) (*BindingFunc, error) {
	res := &BindingFunc{}
	res.Name = fn.Name.RyeName
	res.NameIdent = &fn.Name
	if fn.Recv != nil {
		res.Recv = fn.Recv.RyeName
	}

	var cb CodeBuilder

	params := fn.Params
	if fn.Recv != nil {
		recvName, _ := NewIdent(ctx, nil, &ast.Ident{Name: "__recv"})
		params = append([]NamedIdent{{Name: recvName, Type: *fn.Recv}}, params...)
	}

	if len(params) > 5 {
		return nil, errors.New("can only handle at most 5 parameters")
	}

	res.Doc = FuncGoIdent(fn)
	res.Argsn = len(params)

	derefParam := make([]bool, len(params))

	for i, param := range params {
		typ := param.Type
		if _, ok := ctx.Data.Structs[typ.GoName]; ok {
			var err error
			typ, err = NewIdent(ctx, typ.File, &ast.StarExpr{X: typ.Expr})
			if err != nil {
				panic(err)
			}
			derefParam[i] = true
		}
		cb.Linef(`var arg%vVal %v`, i, typ.GoName)
		ctx.MarkUsed(typ)
		if _, found := ConvRyeToGo(
			ctx,
			&cb,
			typ,
			fmt.Sprintf(`arg%vVal`, i),
			fmt.Sprintf(`arg%v`, i),
			i,
			makeMakeRetArgErr(i),
		); !found {
			return nil, errors.New("unhandled type conversion (rye to go): " + param.Type.GoName)
		}
	}

	var args strings.Builder
	{
		start := 0
		if fn.Recv != nil {
			start = 1
		}
		for i := start; i < len(params); i++ {
			param := params[i]
			if i != start {
				args.WriteString(`, `)
			}
			expand := ""
			if param.Type.IsEllipsis {
				expand = "..."
			}
			deref := ""
			if derefParam[i] {
				deref = "*"
			}
			args.WriteString(fmt.Sprintf(`%varg%vVal%v`, deref, i, expand))
		}
	}

	var assign strings.Builder
	{
		for i := range fn.Results {
			if i != 0 {
				assign.WriteString(`, `)
			}
			assign.WriteString(fmt.Sprintf(`res%v`, i))
		}
		if len(fn.Results) > 0 {
			assign.WriteString(` := `)
		}
	}

	recv := ""
	if fn.Recv != nil {
		if derefParam[0] {
			recv = `(*arg0Val).`
		} else {
			recv = `arg0Val.`
		}
	}
	cb.Linef(`%v%v%v(%v)`, assign.String(), recv, fn.Name.GoName, args.String())
	ctx.MarkUsed(fn.Name)
	if len(fn.Results) > 0 {
		for i, result := range fn.Results {
			addr := ""
			typ := result.Type
			if _, ok := ctx.Data.Structs[typ.GoName]; ok {
				var err error
				typ, err = NewIdent(ctx, typ.File, &ast.StarExpr{X: typ.Expr})
				if err != nil {
					panic(err)
				}
				addr = "&"
			}
			cb.Linef(`var res%vObj env.Object`, i)
			if _, found := ConvGoToRye(
				ctx,
				&cb,
				typ,
				fmt.Sprintf(`res%vObj`, i),
				fmt.Sprintf(`%vres%v`, addr, i),
				-1,
				nil,
			); !found {
				return nil, errors.New("unhandled type conversion (go to rye): " + result.Type.GoName)
			}
		}
		if len(fn.Results) == 1 {
			cb.Linef(`return res0Obj`)
		} else {
			cb.Linef(`return env.NewDict(map[string]any{`)
			cb.Indent++
			for i, result := range fn.Results {
				cb.Linef(`"%v": res%vObj,`, result.Name.RyeName, i)
			}
			cb.Indent--
			cb.Linef(`})`)
		}
	} else {
		if fn.Recv == nil {
			cb.Linef(`return nil`)
		} else {
			cb.Linef(`return arg0`)
		}
	}
	res.Body = cb.String()

	return res, nil
}

func GenerateGetterOrSetter(ctx *Context, field NamedIdent, structName Ident, setter bool) (*BindingFunc, error) {
	res := &BindingFunc{}

	{
		var err error
		structName, err = NewIdent(ctx, structName.File, &ast.StarExpr{X: structName.Expr})
		if err != nil {
			return nil, err
		}
	}

	res.Recv = structName.RyeName
	if setter {
		res.Name = field.Name.RyeName + "!"
	} else {
		res.Name = field.Name.RyeName + "?"
	}

	var cb CodeBuilder

	if setter {
		res.Doc = fmt.Sprintf("Set %v %v value", structName.GoName, field.Name.GoName)
		res.Argsn = 2
	} else {
		res.Doc = fmt.Sprintf("Get %v %v value", structName.GoName, field.Name.GoName)
		res.Argsn = 1
	}

	cb.Linef(`var self %v`, structName.GoName)
	ctx.MarkUsed(structName)
	if _, found := ConvRyeToGo(
		ctx,
		&cb,
		structName,
		`self`,
		`arg0`,
		0,
		makeMakeRetArgErr(0),
	); !found {
		return nil, errors.New("unhandled type conversion (go to rye): " + structName.GoName)
	}

	if setter {
		if _, found := ConvRyeToGo(
			ctx,
			&cb,
			field.Type,
			`self.`+field.Name.GoName,
			`arg1`,
			1,
			makeMakeRetArgErr(1),
		); !found {
			return nil, errors.New("unhandled type conversion (go to rye): " + structName.GoName)
		}

		cb.Linef(`return arg0`)
	} else {
		addr := ""
		typ := field.Type
		if _, ok := ctx.Data.Structs[typ.GoName]; ok {
			var err error
			typ, err = NewIdent(ctx, typ.File, &ast.StarExpr{X: typ.Expr})
			if err != nil {
				panic(err)
			}
			addr = "&"
		}
		cb.Linef(`var resObj env.Object`)
		if _, found := ConvGoToRye(
			ctx,
			&cb,
			typ,
			`resObj`,
			addr+`self.`+field.Name.GoName,
			-1,
			nil,
		); !found {
			return nil, errors.New("unhandled type conversion (go to rye): " + field.Type.GoName)
		}
		cb.Linef(`return resObj`)
	}
	res.Body = cb.String()

	return res, nil
}

func GenerateValue(ctx *Context, value NamedIdent) (*BindingFunc, error) {
	res := &BindingFunc{}
	res.Name = value.Name.RyeName
	res.NameIdent = &value.Name
	res.Doc = fmt.Sprintf("Get %v value", value.Name.GoName)
	res.Argsn = 0

	ctx.MarkUsed(value.Name)

	var cb CodeBuilder

	cb.Linef(`var resObj env.Object`)
	if _, found := ConvGoToRye(
		ctx,
		&cb,
		value.Type,
		`resObj`,
		value.Name.GoName,
		-1,
		nil,
	); !found {
		return nil, errors.New("unhandled type conversion (go to rye): " + value.Type.GoName)
	}
	cb.Linef(`return resObj`)
	res.Body = cb.String()

	return res, nil
}

func GenerateNewStruct(ctx *Context, structName Ident) (*BindingFunc, error) {
	res := &BindingFunc{}
	{
		id, err := NewIdent(ctx, structName.File, &ast.Ident{Name: "New" + structName.Expr.(*ast.Ident).Name})
		if err != nil {
			return nil, err
		}
		res.Name = id.RyeName
		res.NameIdent = &id
	}
	res.Doc = fmt.Sprintf("Create a new %v struct", structName.GoName)
	res.Argsn = 0

	ctx.MarkUsed(structName)

	structPtr, err := NewIdent(ctx, structName.File, &ast.StarExpr{X: structName.Expr})
	if err != nil {
		panic(err)
	}

	var cb CodeBuilder
	cb.Linef(`res := &%v{}`, structName.GoName)
	cb.Linef(`var resObj env.Object`)
	if _, found := ConvGoToRye(
		ctx,
		&cb,
		structPtr,
		`resObj`,
		`res`,
		-1,
		nil,
	); !found {
		return nil, errors.New("unhandled type conversion (go to rye): " + structName.GoName)
	}
	cb.Linef(`return resObj`)
	res.Body = cb.String()

	return res, nil
}

func GenerateGenericInterfaceImpl(ctx *Context, iface *Interface) (string, error) {
	var cb CodeBuilder

	name := "iface_" + strings.ReplaceAll(iface.Name.GoName, ".", "_")
	cb.Linef(`type %v struct {`, name)
	cb.Indent++
	cb.Linef(`self env.RyeCtx`)
	makeFnTyp := func(fn *Func, withSelf, selfAsRecv bool) string {
		var b strings.Builder
		b.WriteString("func")
		if withSelf && selfAsRecv {
			b.WriteString(fmt.Sprintf(" (self *%v) %v", name, fn.Name.GoName))
		}
		b.WriteString("(")
		nParamsW := 0
		if withSelf && !selfAsRecv {
			b.WriteString("self env.RyeCtx")
			nParamsW++
		}
		for i, param := range fn.Params {
			if nParamsW != 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("arg%v %v", i, param.Type.GoName))
			ctx.MarkUsed(param.Type)
			nParamsW++
		}
		b.WriteString(")")
		if len(fn.Results) > 0 {
			b.WriteString(" (")
			for i, result := range fn.Results {
				if i != 0 {
					b.WriteString(", ")
				}
				b.WriteString(result.Type.GoName)
				ctx.MarkUsed(result.Type)
			}
			b.WriteString(")")
		}
		return b.String()
	}
	for _, fn := range iface.Funcs {
		cb.Linef(`fn_%v %v`, fn.Name.GoName, makeFnTyp(fn, true, false))
	}
	cb.Indent--
	cb.Linef(`}`)
	for _, fn := range iface.Funcs {
		cb.Linef(`%v {`, makeFnTyp(fn, true, true))
		cb.Indent++
		var argsB strings.Builder
		argsB.WriteString("self.self")
		for i := range fn.Params {
			argsB.WriteString(", ")
			argsB.WriteString(fmt.Sprintf("arg%v", i))
		}
		var retStmt string
		if len(fn.Results) > 0 {
			retStmt = "return "
		}
		cb.Linef(`%vself.fn_%v(%v)`, retStmt, fn.Name.GoName, argsB.String())
		cb.Indent--
		cb.Linef(`}`)
	}
	cb.Linef(``)

	cb.Linef(`func ctxTo_%v(ps *env.ProgramState, v env.RyeCtx) (%v, error) {`, strings.ReplaceAll(iface.Name.GoName, ".", "_"), iface.Name.GoName)
	cb.Indent++
	ctx.MarkUsed(iface.Name)
	cb.Linef(`words := v.GetWords(*ps.Idx).Series.S`)
	cb.Linef(`wordToObj := make(map[string]env.Object, len(words))`)
	cb.Linef(`for _, word := range words {`)
	cb.Indent++
	cb.Linef(`name := word.(env.String).Value`)
	cb.Linef(`idx, ok := ps.Idx.GetIndex(name)`)
	cb.Linef(`if !ok {`)
	cb.Indent++
	cb.Linef(`panic("expected valid word")`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`obj, ok := v.Get(idx)`)
	cb.Linef(`if !ok {`)
	cb.Indent++
	cb.Linef(`panic("expected valid index")`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`wordToObj[name] = obj`)
	cb.Indent--
	cb.Linef(`}`)
	implTyp := "iface_" + strings.ReplaceAll(iface.Name.GoName, ".", "_")
	cb.Linef(`impl := &%v{`, implTyp)
	cb.Indent++
	cb.Linef(`self: v,`)
	cb.Indent--
	cb.Linef(`}`)
	for i, fn := range iface.Funcs {
		cb.Linef(`ctxObj%v, ok := wordToObj["%v"]`, i, fn.Name.RyeName)
		cb.Linef(`if !ok {`)
		cb.Indent++
		cb.Linef(`return nil, errors.New("context to %v: expected context to have function %v")`, iface.Name.GoName, fn.Name.RyeName)
		ctx.UsedImports["errors"] = struct{}{}
		cb.Indent--
		cb.Linef(`}`)
		if !ConvRyeToGoCodeFunc(
			ctx,
			&cb,
			fmt.Sprintf(`impl.fn_%v`, fn.Name.GoName),
			fmt.Sprintf(`ctxObj%v`, i),
			-1,
			func(inner string) string {
				ctx.UsedImports["errors"] = struct{}{}
				return fmt.Sprintf(`return nil, errors.New("context to %v: context fn %v: %v")`, iface.Name.GoName, fn.Name.RyeName, inner)
			},
			true,
			fn.Params,
			fn.Results,
		) {
			return "", errors.New("unhandled function conversion (rye to go): " + fn.Name.GoName)
		}
	}
	cb.Linef(`return impl, nil`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	return cb.String(), nil
}

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

func Run() {
	configPath := "config.toml"
	if _, err := os.Stat(configPath); err != nil {
		if err := os.WriteFile(configPath, []byte(DefaultConfig), 0666); err != nil {
			fmt.Println("create default config:", err)
			os.Exit(1)
		}
		fmt.Println("created default config at", configPath)
		os.Exit(0)
	}
	var cfg Config
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

	moduleNames := make(map[string]string) // module path to name
	moduleDirPaths := make(map[string]string)
	{
		addPkgNames := func(dir, modulePath string) (string, []module.Version, error) {
			goVer, pkgNms, req, err := ParseDirModules(fset, dir, modulePath)
			if err != nil {
				return "", nil, err
			}
			for mod, name := range pkgNms {
				moduleNames[mod] = name
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
	moduleImportNames := make(map[string]string) // module path to name; each name value is unique
	moduleImportNames["C"] = "C"
	{
		moduleNameKeys := make([]string, 0, len(moduleNames))
		for k := range moduleNames {
			moduleNameKeys = append(moduleNameKeys, k)
		}
		slices.SortFunc(moduleNameKeys, makeCompareModulePaths(cfg.Package))

		moduleNameIdxs := make(map[string]int) // module name to number of occurrences
		for _, mod := range moduleNameKeys {
			name := moduleNames[mod]
			impName := name
			if idx := moduleNameIdxs[name]; idx > 0 {
				impName += "_" + strconv.Itoa(idx)
			}
			moduleImportNames[mod] = impName
			moduleNameIdxs[name]++
		}
	}

	startTime := time.Now()

	parsedPkgs := make(map[string]struct{})
	genBindingPkgs := make(map[string]struct{}) // mod paths
	data := &Data{
		Funcs:          make(map[string]*Func),
		Interfaces:     make(map[string]*Interface),
		Structs:        make(map[string]*Struct),
		Typedefs:       make(map[string]Ident),
		Values:         make(map[string]NamedIdent),
		RequiredPkgs:   make(map[string]struct{}),
		RequiredIfaces: make(map[string]*Interface),
	}
	ctx := &Context{
		Config:      &cfg,
		Data:        data,
		ModuleNames: moduleImportNames,
		UsedImports: make(map[string]struct{}),
		UsedTyps:    make(map[string]Ident),
	}

	parseDir := func(dirPath string, modulePath string, genBinding, typeDeclsOnly bool) {
		pkgs, err := ParseDir(fset, dirPath, modulePath)
		if err != nil {
			fmt.Println("parse source:", err)
			os.Exit(1)
		}

		for _, pkg := range pkgs {
			for name, f := range pkg.Files {
				name = strings.TrimPrefix(name, dstPath+string(filepath.Separator))
				tdo := typeDeclsOnly
				if ModulePathIsInternal(ctx, pkg.Path) {
					tdo = true
				}
				if err := data.AddFile(ctx, f, name, pkg.Path, moduleNames, tdo); err != nil {
					fmt.Printf("%v: %v\n", pkg.Name, err)
				}
			}
			if genBinding {
				genBindingPkgs[pkg.Path] = struct{}{}
			}
			parsedPkgs[pkg.Path] = struct{}{}
		}
	}

	parseDir(srcDir, cfg.Package, true, false)

	for mod := range data.RequiredPkgs {
		if _, ok := parsedPkgs[mod]; ok {
			continue
		}
		parseDir(moduleDirPaths[mod], mod, false, true)
	}

	if err := data.ResolveInheritancesAndMethods(ctx); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	bindingFuncs := make(map[string]*BindingFunc)

	for _, iface := range data.Interfaces {
		if iface.Name.File == nil || IdentIsInternal(ctx, iface.Name) {
			continue
		}
		if _, ok := genBindingPkgs[iface.Name.File.ModulePath]; !ok {
			continue
		}
		for _, fn := range iface.Funcs {
			bind, err := GenerateBinding(ctx, fn)
			if err != nil {
				fmt.Println(fn.String()+":", err)
				continue
			}
			bindingFuncs[bind.FullName()] = bind
		}
	}

	for _, fn := range data.Funcs {
		if IdentIsInternal(ctx, fn.Name) || (fn.Recv != nil && IdentIsInternal(ctx, *fn.Recv)) {
			continue
		}
		if _, ok := genBindingPkgs[fn.File.ModulePath]; !ok {
			continue
		}
		bind, err := GenerateBinding(ctx, fn)
		if err != nil {
			fmt.Println(fn.String()+":", err)
			continue
		}
		bindingFuncs[bind.FullName()] = bind
	}

	for _, struc := range data.Structs {
		if struc.Name.File == nil || IdentIsInternal(ctx, struc.Name) {
			continue
		}
		if _, ok := genBindingPkgs[struc.Name.File.ModulePath]; !ok {
			continue
		}
		for _, f := range struc.Fields {
			for _, setter := range []bool{false, true} {
				bind, err := GenerateGetterOrSetter(ctx, f, struc.Name, setter)
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

	for _, value := range data.Values {
		if value.Name.File == nil || IdentIsInternal(ctx, value.Name) {
			continue
		}
		if _, ok := genBindingPkgs[value.Name.File.ModulePath]; !ok {
			continue
		}
		bind, err := GenerateValue(ctx, value)
		if err != nil {
			s := value.Name.RyeName
			fmt.Println(s+":", err)
			continue
		}
		bindingFuncs[bind.FullName()] = bind
	}

	for _, struc := range data.Structs {
		if struc.Name.File == nil || IdentIsInternal(ctx, struc.Name) {
			continue
		}
		if _, ok := genBindingPkgs[struc.Name.File.ModulePath]; !ok {
			continue
		}
		bind, err := GenerateNewStruct(ctx, struc.Name)
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
		for name, iface := range data.RequiredIfaces {
			ifaceImpl, err := GenerateGenericInterfaceImpl(ctx, iface)
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
	var bindingList *BindingList
	if _, err := os.Stat(bindingListPath); err == nil {
		var err error
		bindingList, err = LoadBindingListFromFile(bindingListPath)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	} else {
		bindingList = NewBindingList()
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
	addBindingFuncPrio := func(bind *BindingFunc) {
		if bind.NameIdent == nil || bind.Recv != "" {
			return
		}

		name, file := bind.SplitGoNameAndMod()
		if ctx.Config.CutNew {
			name = strcase.ToKebab(strings.TrimPrefix(name, "New"))
			if name == "" {
				name = strcase.ToKebab(ctx.ModuleNames[file.ModulePath])
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
				newName = strcase.ToKebab(ctx.ModuleNames[file.ModulePath])
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
				moduleName := ctx.ModuleNames[file.ModulePath]
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

	rootModuleName := moduleNames[cfg.Package]
	buildFlag := strings.Replace(cfg.BuildFlag, "*", rootModuleName, 1)

	outDir := filepath.Join(cfg.OutDir, rootModuleName)
	if err := os.MkdirAll(outDir, os.ModePerm); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	outFileNot := filepath.Join(outDir, "builtins.not.go")
	outFile := filepath.Join(outDir, "builtins.go")

	var cb CodeBuilder

	cb.Linef(`// Code generated by ryegen. DO NOT EDIT.`)
	cb.Linef(``)
	cb.Linef(`//go:build !%v`, buildFlag)
	cb.Linef(``)
	cb.Linef(`package %v`, rootModuleName)
	cb.Linef(``)
	cb.Linef(`import "github.com/refaktor/rye/env"`)
	cb.Linef(``)
	cb.Linef(`var Builtins = map[string]*env.Builtin{}`)

	cb.SaveToFile(outFileNot, true)
	cb.Reset()

	cb.Linef(`// Code generated by ryegen. DO NOT EDIT.`)
	cb.Linef(``)
	cb.Linef(`//go:build %v`, buildFlag)
	cb.Linef(``)
	cb.Linef(`package %v`, rootModuleName)
	cb.Linef(``)
	cb.Linef(`import (`)
	cb.Indent++
	usedImportKeys := make([]string, 0, len(ctx.UsedImports))
	for k := range ctx.UsedImports {
		usedImportKeys = append(usedImportKeys, k)
	}
	slices.Sort(usedImportKeys)
	for _, mod := range usedImportKeys {
		name := moduleNames[mod]
		impName := moduleImportNames[mod]
		if name == impName {
			cb.Linef(`"%v"`, mod)
		} else {
			cb.Linef(`%v "%v"`, impName, mod)
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
		typNames := make(map[string]string, len(data.Structs)*2)
		for _, struc := range data.Structs {
			id := struc.Name
			if !IdentExprIsExported(id.Expr) || IdentIsInternal(ctx, id) {
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
		strucNameKeys := make([]string, 0, len(typNames))
		for k := range typNames {
			strucNameKeys = append(strucNameKeys, k)
		}
		slices.Sort(strucNameKeys)
		for _, k := range strucNameKeys {
			cb.Linef(`"%v": "%v",`, k, typNames[k])
		}
	}
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	requiredIfaceImplKeys := make([]string, 0, len(data.RequiredIfaces))
	for k := range data.RequiredIfaces {
		requiredIfaceImplKeys = append(requiredIfaceImplKeys, k)
	}
	slices.Sort(requiredIfaceImplKeys)
	for _, key := range requiredIfaceImplKeys {
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
	bindingFuncKeys := make([]string, 0, len(bindingFuncs))
	for k := range bindingFuncs {
		bindingFuncKeys = append(bindingFuncKeys, k)
	}
	slices.Sort(bindingFuncKeys)
	for _, k := range bindingFuncKeys {
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
	if err := cb.SaveToFile(outFile, true); err != nil {
		fmt.Println("save bindings:", err)
		os.Exit(1)
	}
	log.Printf("Wrote bindings to %v", outFile)
}
