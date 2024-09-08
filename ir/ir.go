package ir

import (
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/iancoleman/strcase"
)

// Module path to globally unique name.
type UniqueModuleNames map[string]string

type File struct {
	Name          string
	ModuleName    string
	ModulePath    string
	ImportsByName map[string]*File
	ImportsByPath map[string]*File
}

func (f *File) AddImport(imp *File) {
	f.ImportsByName[imp.ModuleName] = imp
	f.ImportsByPath[imp.ModulePath] = imp
}

func IdentExprIsExported(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.Ident:
		return token.IsExported(expr.Name)
	case *ast.StarExpr:
		return IdentExprIsExported(expr.X)
	case *ast.SelectorExpr:
		return IdentExprIsExported(expr.Sel)
	case *ast.ArrayType:
		return IdentExprIsExported(expr.Elt)
	case *ast.Ellipsis:
		return IdentExprIsExported(expr.Elt)
	default:
		return false
	}
}

func ModulePathIsInternal(modNames UniqueModuleNames, p string) bool {
	if _, ok := modNames[p]; !ok {
		return false
	}
	sp := strings.Split(p, "/")
	for _, elem := range sp {
		if elem == "internal" {
			return true
		}
	}
	return false
}

func IdentIsInternal(modNames UniqueModuleNames, id Ident) bool {
	if id.File == nil {
		return false
	}
	for _, imp := range id.UsedImports {
		if ModulePathIsInternal(modNames, imp.ModulePath) {
			return true
		}
	}
	return false
}

type Ident struct {
	Expr        ast.Expr
	GoName      string
	RyeName     string
	IsEllipsis  bool
	File        *File
	UsedImports []*File
}

func identExprToRyeName(modNames UniqueModuleNames, file *File, expr ast.Expr) (string, error) {
	// From https://github.com/refaktor/rye/blob/main/loader/loader.go#L444
	// WORD          <-  LETTER LETTERORNUM* / NORMOPWORDS
	// LETTER        <-  < [a-zA-Z^(` + "`" + `] >
	// LETTERORNUM   <-  < [a-zA-Z0-9-?=.\\!_+<>\]*()] >
	// NORMOPWORDS   <-  < ("_"[<>*+-=/]) >

	switch expr := expr.(type) {
	case *ast.Ident:
		res := strcase.ToKebab(expr.Name)
		if ast.IsExported(expr.Name) && file != nil {
			res = strcase.ToKebab(modNames[file.ModulePath]) + "-" + res
		}
		return res, nil
	case *ast.StarExpr:
		res, err := identExprToRyeName(modNames, file, expr.X)
		return "ptr-" + res, err
	case *ast.SelectorExpr:
		mod, ok := expr.X.(*ast.Ident)
		if !ok {
			panic("expected ast.SelectorExpr.X to be of type *ast.Ident")
		}
		f, ok := file.ImportsByName[mod.Name]
		if !ok {
			return "", fmt.Errorf("module %v imported by %v not found", mod.Name, file.Name)
		}
		return identExprToRyeName(modNames, f, expr.Sel)
	case *ast.ArrayType:
		if expr.Len != nil {
			return "", errors.New("invalid array length type " + reflect.TypeOf(expr.Len).String())
		}
		res, err := identExprToRyeName(modNames, file, expr.Elt)
		return "arr-" + res, err
	case *ast.Ellipsis:
		res, err := identExprToRyeName(modNames, file, expr.Elt)
		return "arr-" + res, err
	case *ast.FuncType:
		if expr.TypeParams != nil {
			return "", errors.New("generic functions as parameters are unsupported")
		}

		var res strings.Builder

		params, _, err := ParamsToIdents(modNames, file, expr.Params)
		if err != nil {
			return "", err
		}
		res.WriteString("func(")
		for i, v := range params {
			if i != 0 {
				res.WriteString("_")
			}
			res.WriteString(v.Type.RyeName)
		}
		res.WriteString(")")

		if expr.Results != nil {
			results, _, err := ParamsToIdents(modNames, file, expr.Results)
			if err != nil {
				return "", err
			}
			res.WriteString("_(")
			for i, v := range results {
				if i != 0 {
					res.WriteString("_")
				}
				res.WriteString(v.Type.RyeName)
			}
			res.WriteString(")")
		}

		return res.String(), nil
	case *ast.MapType:
		key, err := identExprToRyeName(modNames, file, expr.Key)
		if err != nil {
			return "", err
		}
		val, err := identExprToRyeName(modNames, file, expr.Value)
		if err != nil {
			return "", err
		}
		return "map(" + key + ")" + val, nil
	case *ast.InterfaceType:
		if len(expr.Methods.List) == 0 {
			return "any", nil
		}
		return "", errors.New("non-empty inline interfaces not supported")
	case *ast.ChanType:
		val, err := identExprToRyeName(modNames, file, expr.Value)
		if err != nil {
			return "", err
		}
		ch := "chan"
		send := ""
		if expr.Dir&ast.SEND != 0 {
			send = "s"
		}
		recv := ""
		if expr.Dir&ast.RECV != 0 {
			recv = "r"
		}
		if send != "" || recv != "" {
			ch += "_" + send + recv
		}
		return ch + "_" + val, nil
	default:
		return "", errors.New("invalid identifier expression type " + reflect.TypeOf(expr).String())
	}
}

func identExprToGoName(modNames UniqueModuleNames, file *File, expr ast.Expr) (ident string, usedImports []*File, err error) {
	switch expr := expr.(type) {
	case *ast.Ident:
		if file != nil {
			if ast.IsExported(expr.Name) {
				mod, ok := modNames[file.ModulePath]
				if !ok {
					return "", nil, fmt.Errorf("unknown module path %v", file.ModulePath)
				}
				return mod + "." + expr.Name, []*File{file}, nil
			}
		}
		if ast.IsExported(expr.Name) {
			return expr.Name, []*File{file}, nil
		} else {
			return expr.Name, nil, nil
		}
	case *ast.StarExpr:
		res, imps, err := identExprToGoName(modNames, file, expr.X)
		return "*" + res, imps, err
	case *ast.SelectorExpr:
		mod, ok := expr.X.(*ast.Ident)
		if !ok {
			panic("expected ast.SelectorExpr.X to be of type *ast.Ident")
		}
		f, ok := file.ImportsByName[mod.Name]
		if !ok {
			return "", nil, fmt.Errorf("module %v imported by %v not found", mod.Name, file.Name)
		}
		res, imps, err := identExprToGoName(modNames, f, expr.Sel)
		return res, imps, err
	case *ast.ArrayType:
		if expr.Len != nil {
			return "", nil, errors.New("invalid array length type " + reflect.TypeOf(expr.Len).String())
		}
		res, imps, err := identExprToGoName(modNames, file, expr.Elt)
		return "[]" + res, imps, err
	case *ast.Ellipsis:
		res, imps, err := identExprToGoName(modNames, file, expr.Elt)
		return "[]" + res, imps, err
	case *ast.FuncType:
		if expr.TypeParams != nil {
			return "", nil, errors.New("generic functions as parameters are unsupported")
		}

		var res strings.Builder

		params, imps, err := ParamsToIdents(modNames, file, expr.Params)
		if err != nil {
			return "", nil, err
		}
		res.WriteString("func(")
		for i, v := range params {
			if i != 0 {
				res.WriteString(", ")
			}
			res.WriteString(v.Type.GoName)
		}
		res.WriteString(")")

		if expr.Results != nil {
			results, impsR, err := ParamsToIdents(modNames, file, expr.Results)
			if err != nil {
				return "", nil, err
			}
			imps = append(imps, impsR...)
			res.WriteString(" (")
			for i, v := range results {
				if i != 0 {
					res.WriteString(", ")
				}
				res.WriteString(v.Type.GoName)
			}
			res.WriteString(")")
		}

		return res.String(), imps, nil
	case *ast.MapType:
		key, impsK, err := identExprToGoName(modNames, file, expr.Key)
		if err != nil {
			return "", nil, err
		}
		val, impsV, err := identExprToGoName(modNames, file, expr.Value)
		if err != nil {
			return "", nil, err
		}
		return "map[" + key + "]" + val, append(impsK, impsV...), nil
	case *ast.InterfaceType:
		if len(expr.Methods.List) == 0 {
			return "any", nil, nil
		}
		return "", nil, errors.New("non-empty inline interfaces not supported")
	case *ast.ChanType:
		val, imps, err := identExprToGoName(modNames, file, expr.Value)
		if err != nil {
			return "", nil, err
		}
		ch := "chan"
		if !(expr.Dir&ast.SEND != 0 && expr.Dir&ast.RECV != 0) {
			if expr.Dir&ast.RECV != 0 {
				ch = "<-" + ch
			}
			if expr.Dir&ast.SEND != 0 {
				ch = ch + "<-"
			}
		}
		return ch + " " + val, imps, nil
	default:
		return "", nil, errors.New("invalid identifier expression type " + reflect.TypeOf(expr).String())
	}
}

func NewIdent(modNames UniqueModuleNames, file *File, expr ast.Expr) (Ident, error) {
	goName, imps, err := identExprToGoName(modNames, file, expr)
	if err != nil {
		return Ident{}, err
	}
	ryeName, err := identExprToRyeName(modNames, file, expr)
	if err != nil {
		return Ident{}, err
	}
	isEllipsis := false
	if _, ok := expr.(*ast.Ellipsis); ok {
		isEllipsis = true
	}
	return Ident{
		Expr:        expr,
		GoName:      goName,
		RyeName:     ryeName,
		IsEllipsis:  isEllipsis,
		File:        file,
		UsedImports: imps,
	}, nil
}

// Returns *SelectorExpr referenced *File, true. If id is not *SelectorExpr, returns nil, false.
func (id *Ident) GetReferencedPackage(modNames UniqueModuleNames, file *File) (*File, bool) {
	se, ok := id.Expr.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	x, ok := se.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	return file.ImportsByName[x.Name], true
}

type Func struct {
	Name    Ident
	Recv    *Ident // non-nil for methods
	Params  []NamedIdent
	Results []NamedIdent
	File    *File
}

func NewFunc(modNames UniqueModuleNames, file *File, fd *ast.FuncDecl) (*Func, error) {
	var err error
	res := &Func{
		File: file,
	}
	if fd.Recv == nil {
		res.Name, err = NewIdent(modNames, file, fd.Name)
		if err != nil {
			return nil, err
		}
	} else {
		res.Name, err = NewIdent(modNames, nil, fd.Name)
		if err != nil {
			return nil, err
		}
		if len(fd.Recv.List) != 1 {
			panic("expected exactly one receiver in method")
		}
		id, err := NewIdent(modNames, file, fd.Recv.List[0].Type)
		if err != nil {
			return nil, err
		}
		res.Recv = &id
	}
	fn := fd.Type
	{
		ids, _, err := ParamsToIdents(modNames, file, fn.Params)
		if err != nil {
			return nil, err
		}
		res.Params = ids
	}
	if fn.Results != nil {
		ids, _, err := ParamsToIdents(modNames, file, fn.Results)
		if err != nil {
			return nil, err
		}
		res.Results = ids
	}
	return res, nil
}

func (fn *Func) String() string {
	var b strings.Builder
	if fn.Recv != nil {
		b.WriteString("(")
		b.WriteString(fn.Recv.GoName)
		//b.WriteString("/")
		//b.WriteString(fn.Recv.RyeName)
		b.WriteString(") ")
	}
	b.WriteString(fn.Name.GoName)
	//b.WriteString("/")
	//b.WriteString(fn.Name.RyeName)
	b.WriteString(" ")
	b.WriteString("(")
	for i, v := range fn.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(v.Type.GoName)
		//b.WriteString("/")
		//b.WriteString(v.RyeName)
	}
	b.WriteString(") -> (")
	for i, v := range fn.Results {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(v.Type.GoName)
		//b.WriteString("/")
		//b.WriteString(v.RyeName)
	}
	b.WriteString(")")
	return b.String()
}

func ParamsToIdents(modNames UniqueModuleNames, file *File, fl *ast.FieldList) (idents []NamedIdent, substImports []*File, err error) {
	var res []NamedIdent
	var substImps []*File
	for i, v := range fl.List {
		typID, err := NewIdent(modNames, file, v.Type)
		if err != nil {
			return nil, nil, err
		}
		substImps = append(substImps, typID.UsedImports...)
		if len(v.Names) > 0 {
			for _, n := range v.Names {
				nameID, err := NewIdent(modNames, nil, n)
				if err != nil {
					return nil, nil, err
				}
				res = append(res, NamedIdent{
					Name: nameID,
					Type: typID,
				})
			}
		} else {
			var shorthand string
			if typID.GoName == "error" && i == len(fl.List)-1 {
				shorthand = "err"
			} else {
				shorthand = strconv.Itoa(i + 1)
			}
			nameID, err := NewIdent(modNames, nil, &ast.Ident{Name: shorthand})
			if err != nil {
				return nil, nil, err
			}
			res = append(res, NamedIdent{
				Name: nameID,
				Type: typID,
			})
		}
	}
	return res, substImps, nil
}

type NamedIdent struct {
	Name Ident
	Type Ident
}

type Struct struct {
	Name     Ident
	Fields   []NamedIdent
	Methods  map[string]*Func
	Inherits []Ident
}

func NewStruct(modNames UniqueModuleNames, file *File, name *ast.Ident, structTyp *ast.StructType) (*Struct, error) {
	res := &Struct{
		Methods: make(map[string]*Func),
	}
	{
		id, err := NewIdent(modNames, file, name)
		if err != nil {
			return nil, err
		}
		res.Name = id
	}
	for _, f := range structTyp.Fields.List {
		if len(f.Names) > 0 {
			typID, err := NewIdent(modNames, file, f.Type)
			if err != nil {
				return nil, err
			}

			for _, name := range f.Names {
				if !name.IsExported() {
					continue
				}
				nameID, err := NewIdent(modNames, nil, name)
				if err != nil {
					return nil, err
				}
				res.Fields = append(res.Fields, NamedIdent{
					Name: nameID,
					Type: typID,
				})
			}
		} else {
			typ := f.Type
			if se, ok := f.Type.(*ast.StarExpr); ok {
				typ = se.X
			}
			if !IdentExprIsExported(typ) {
				continue
			}
			id, err := NewIdent(modNames, file, typ)
			if err != nil {
				return nil, err
			}
			res.Inherits = append(res.Inherits, id)
		}
	}
	return res, nil
}

type Interface struct {
	Name             Ident
	Funcs            []*Func
	Inherits         []Ident
	HasPrivateFields bool
}

func funcFromInterfaceField(modNames UniqueModuleNames, file *File, ifaceIdent Ident, f *ast.Field) (*Func, error) {
	var err error
	res := &Func{
		File: file,
	}
	if len(f.Names) != 1 {
		panic("expected interface method to have 1 name")
	}
	res.Name, err = NewIdent(modNames, nil, f.Names[0])
	if err != nil {
		return nil, err
	}
	res.Recv = &ifaceIdent
	fn, ok := f.Type.(*ast.FuncType)
	if !ok {
		panic("expected method type to be of type *ast.FuncType")
	}
	{
		ids, _, err := ParamsToIdents(modNames, file, fn.Params)
		if err != nil {
			return nil, err
		}
		res.Params = ids
	}
	if fn.Results != nil {
		ids, _, err := ParamsToIdents(modNames, file, fn.Results)
		if err != nil {
			return nil, err
		}
		res.Results = ids
	}
	return res, nil
}

func NewInterface(modNames UniqueModuleNames, file *File, name *ast.Ident, ifaceTyp *ast.InterfaceType) (*Interface, error) {
	res := &Interface{}
	{
		id, err := NewIdent(modNames, file, name)
		if err != nil {
			return nil, err
		}
		res.Name = id
	}
	for _, f := range ifaceTyp.Methods.List {
		switch ft := f.Type.(type) {
		case *ast.FuncType:
			if len(f.Names) != 1 {
				panic("expected interface method to have 1 name")
			}
			if !f.Names[0].IsExported() {
				res.HasPrivateFields = true
				continue
			}
			fn, err := funcFromInterfaceField(modNames, file, res.Name, f)
			if err != nil {
				fmt.Println("i2fs:", res.Name.GoName+":", err)
				continue
			}
			res.Funcs = append(res.Funcs, fn)
		case *ast.Ident, *ast.SelectorExpr:
			id, err := NewIdent(modNames, file, ft)
			if err != nil {
				return nil, err
			}
			res.Inherits = append(res.Inherits, id)
		default:
			var s strings.Builder
			format.Node(&s, token.NewFileSet(), f.Type)
			return nil, errors.New("invalid interface field " + s.String())
		}
	}
	return res, nil
}

func FuncGoIdent(fn *Func) string {
	res := fn.Name.GoName
	if fn.Recv != nil {
		_, recvIsPtr := fn.Recv.Expr.(*ast.StarExpr)
		recv := fn.Recv.GoName
		if recvIsPtr {
			recv = "(" + recv + ")"
		}
		res = recv + "." + res
	}
	return res
}

type IR struct {
	Funcs        map[string]*Func
	Interfaces   map[string]*Interface
	Structs      map[string]*Struct
	Typedefs     map[string]Ident
	Values       map[string]NamedIdent // consts and vars
	RequiredPkgs map[string]struct{}   // packages needed for interface/struct inheritance resolution
}

func New() *IR {
	return &IR{
		Funcs:        make(map[string]*Func),
		Interfaces:   make(map[string]*Interface),
		Structs:      make(map[string]*Struct),
		Typedefs:     make(map[string]Ident),
		Values:       make(map[string]NamedIdent),
		RequiredPkgs: make(map[string]struct{}),
	}
}

func (ir *IR) AddFile(modNames UniqueModuleNames, f *ast.File, fName string, modulePath string, modDefaultNames map[string]string, typeDeclsOnly bool) error {
	file := &File{
		Name:          fName,
		ModuleName:    f.Name.Name,
		ModulePath:    modulePath,
		ImportsByName: make(map[string]*File),
		ImportsByPath: make(map[string]*File),
	}

	for _, imp := range f.Imports {
		var name string
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			if v, ok := modDefaultNames[path]; ok {
				name = v
			} else {
				pathElems := strings.Split(path, "/")
				if len(pathElems) == 0 {
					return fmt.Errorf("unable to get module name: invalid import path %v (imported by %v)", path, modulePath)
				}
				if strings.Contains(pathElems[0], ".") {
					// not part of go std, should have been in moduleNames
					return fmt.Errorf("unable to get module name: unknown package %v (imported by %v)", path, modulePath)
				}
				// go std module
				name = pathElems[len(pathElems)-1]
			}
		}
		file.AddImport(&File{
			ModuleName:    name,
			ModulePath:    path,
			ImportsByName: make(map[string]*File),
			ImportsByPath: make(map[string]*File),
		})
	}

declsLoop:
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if typeDeclsOnly {
				continue
			}
			if !decl.Name.IsExported() {
				continue
			}
			if decl.Recv != nil {
				if len(decl.Recv.List) != 1 {
					panic("expected exactly one receiver in method")
				}
				if !IdentExprIsExported(decl.Recv.List[0].Type) {
					continue
				}
			}
			fn, err := NewFunc(modNames, file, decl)
			if err != nil {
				fmt.Println("parse "+file.ModuleName+":", err)
				continue
				//return err
			}
			ir.Funcs[FuncGoIdent(fn)] = fn
		case *ast.GenDecl:
			if decl.Tok == token.CONST || decl.Tok == token.VAR {
				if typeDeclsOnly {
					continue
				}
				var typ *Ident
				for _, spec := range decl.Specs {
					if valSpec, ok := spec.(*ast.ValueSpec); ok {
						if valSpec.Type != nil {
							newTyp, err := NewIdent(modNames, file, valSpec.Type)
							if err != nil {
								fmt.Println("const/var decl:", err)
								continue declsLoop
								//return err
							}
							typ = &newTyp
						}
						if typ == nil {
							continue
						}
						for _, specName := range valSpec.Names {
							if !specName.IsExported() {
								continue
							}
							name, err := NewIdent(modNames, file, specName)
							if err != nil {
								fmt.Println("const/var decl:", err)
								continue declsLoop
								//return err
							}
							ir.Values[name.GoName] = NamedIdent{
								Type: *typ,
								Name: name,
							}
						}
					}
				}
			} else if decl.Tok == token.TYPE {
				if typeSpec, ok := decl.Specs[0].(*ast.TypeSpec); ok {
					if !typeSpec.Name.IsExported() {
						continue
					}
					switch typ := typeSpec.Type.(type) {
					case *ast.InterfaceType:
						iface, err := NewInterface(modNames, file, typeSpec.Name, typ)
						if err != nil {
							return err
						}
						ir.Interfaces[iface.Name.GoName] = iface
						for _, id := range iface.Inherits {
							if refF, ok := id.GetReferencedPackage(modNames, iface.Name.File); ok {
								ir.RequiredPkgs[refF.ModulePath] = struct{}{}
							}
						}
					case *ast.StructType:
						struc, err := NewStruct(modNames, file, typeSpec.Name, typ)
						if err != nil {
							fmt.Println("struct decl:", err)
							continue
							//return err
						}
						ir.Structs[struc.Name.GoName] = struc
						for _, id := range struc.Inherits {
							if refF, ok := id.GetReferencedPackage(modNames, struc.Name.File); ok {
								ir.RequiredPkgs[refF.ModulePath] = struct{}{}
							}
						}
					default:
						name, err := NewIdent(modNames, file, typeSpec.Name)
						if err != nil {
							return err
						}
						id, err := NewIdent(modNames, file, typ)
						if err != nil {
							return err
						}
						ir.Typedefs[name.GoName] = id
					}
				}
			}
		}
	}
	return nil
}

// Resolves interface, struct, and method inheritance
func (ir *IR) ResolveInheritancesAndMethods(modNames UniqueModuleNames) error {
	var resolveInheritedIfaces func(iface *Interface) error
	resolveInheritedIfaces = func(iface *Interface) error {
		for _, inh := range iface.Inherits {
			inhIface, exists := ir.Interfaces[inh.GoName]
			if !exists {
				fmt.Println(errors.New("cannot resolve interface inheritance " + inh.GoName + " in " + iface.Name.GoName + ": does not exist"))
				continue
				//return
			}
			if err := resolveInheritedIfaces(inhIface); err != nil {
				return err
			}
			iface.Funcs = append(iface.Funcs, inhIface.Funcs...)
			iface.Inherits = nil
		}
		return nil
	}
	for _, iface := range ir.Interfaces {
		if err := resolveInheritedIfaces(iface); err != nil {
			return err
		}
	}

	for _, fn := range ir.Funcs {
		if fn.Recv == nil {
			continue
		}
		var recv Ident
		if expr, ok := fn.Recv.Expr.(*ast.StarExpr); ok {
			var err error
			recv, err = NewIdent(modNames, fn.Recv.File, expr.X)
			if err != nil {
				return err
			}
		} else {
			recv = *fn.Recv
		}
		struc, ok := ir.Structs[recv.GoName]
		if !ok {
			fmt.Println(errors.New("function " + FuncGoIdent(fn) + " from " + fn.File.ModulePath + " has unknown receiver struct " + recv.GoName))
			continue
			//return
		}
		struc.Methods[fn.Name.GoName] = fn
	}

	var resolveInheritedStructs func(struc *Struct) error
	resolveInheritedStructs = func(struc *Struct) error {
		for _, inh := range struc.Inherits {
			if inhStruc, exists := ir.Structs[inh.GoName]; exists {
				if err := resolveInheritedStructs(inhStruc); err != nil {
					return err
				}
				struc.Fields = append(struc.Fields, inhStruc.Fields...)
				for name, meth := range inhStruc.Methods {
					if _, exists := struc.Methods[name]; !exists {
						m := &Func{
							Name:    meth.Name,
							Recv:    &struc.Name,
							Params:  slices.Clone(meth.Params),
							Results: slices.Clone(meth.Results),
							File:    struc.Name.File,
						}

						if _, ok := meth.Recv.Expr.(*ast.StarExpr); ok {
							recv, err := NewIdent(modNames, struc.Name.File, &ast.StarExpr{X: struc.Name.Expr})
							if err != nil {
								panic(err)
							}
							m.Recv = &recv
						} else {
							m.Recv = &struc.Name
						}
						struc.Methods[name] = m

						ir.Funcs[FuncGoIdent(m)] = m
					}
				}
			} else if _, exists := ir.Typedefs[inh.GoName]; exists {
				var fieldName string
				if id, ok := inh.Expr.(*ast.Ident); ok {
					fieldName = id.Name
				} else if se, ok := inh.Expr.(*ast.SelectorExpr); ok {
					fieldName = se.Sel.Name
				}
				name, err := NewIdent(modNames, nil, &ast.Ident{Name: fieldName})
				if err != nil {
					return err
				}
				struc.Fields = append(struc.Fields, NamedIdent{
					Name: name,
					Type: inh,
				})
			} else {
				fmt.Println(errors.New("cannot resolve struct inheritance " + inh.GoName + " in " + struc.Name.GoName + ": does not exist"))
				continue
			}
			struc.Inherits = nil
		}
		return nil
	}
	for _, struc := range ir.Structs {
		if err := resolveInheritedStructs(struc); err != nil {
			return err
		}
	}
	return nil
}
