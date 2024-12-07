package ir

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
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
	Name        string
	IsEllipsis  bool
	File        *File
	UsedImports []*File
}

func identExprToGoName(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, expr ast.Expr) (ident string, usedImports []*File, err error) {
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
		res, imps, err := identExprToGoName(constValues, modNames, file, expr.X)
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
		res, imps, err := identExprToGoName(constValues, modNames, f, expr.Sel)
		return res, imps, err
	case *ast.ArrayType:
		lenStr := ""
		if expr.Len != nil {
			lenConst, err := EvalConstExpr(constValues, modNames, file, expr.Len)
			if err != nil {
				return "", nil, err
			}
			lenI := constant.Val(lenConst)
			l, ok := lenI.(int64)
			if !ok {
				return "", nil, fmt.Errorf("invalid fixed-size array length type %T", lenI)
			}
			lenStr = strconv.FormatInt(l, 10)
		}
		res, imps, err := identExprToGoName(constValues, modNames, file, expr.Elt)
		return "[" + lenStr + "]" + res, imps, err
	case *ast.Ellipsis:
		res, imps, err := identExprToGoName(constValues, modNames, file, expr.Elt)
		return "[]" + res, imps, err
	case *ast.FuncType:
		if expr.TypeParams != nil {
			return "", nil, errors.New("generic functions as parameters are unsupported")
		}

		var res strings.Builder

		params, imps, err := ParamsToIdents(constValues, modNames, file, expr.Params)
		if err != nil {
			return "", nil, err
		}
		res.WriteString("func(")
		for i, v := range params {
			if i != 0 {
				res.WriteString(", ")
			}
			res.WriteString(v.Type.ParamName())
		}
		res.WriteString(")")

		if expr.Results != nil {
			results, impsR, err := ParamsToIdents(constValues, modNames, file, expr.Results)
			if err != nil {
				return "", nil, err
			}
			imps = append(imps, impsR...)
			res.WriteString(" (")
			for i, v := range results {
				if i != 0 {
					res.WriteString(", ")
				}
				res.WriteString(v.Type.Name)
			}
			res.WriteString(")")
		}

		return res.String(), imps, nil
	case *ast.MapType:
		key, impsK, err := identExprToGoName(constValues, modNames, file, expr.Key)
		if err != nil {
			return "", nil, err
		}
		val, impsV, err := identExprToGoName(constValues, modNames, file, expr.Value)
		if err != nil {
			return "", nil, err
		}
		return "map[" + key + "]" + val, append(impsK, impsV...), nil
	case *ast.InterfaceType:
		var res strings.Builder
		var imps []*File
		fmt.Fprintf(&res, "interface{")
		for i, meth := range expr.Methods.List {
			if i != 0 {
				fmt.Fprintf(&res, "; ")
			}
			for i, name := range meth.Names {
				if i != 0 {
					fmt.Fprintf(&res, ", ")
				}
				fmt.Fprintf(&res, "%v", name.Name)
			}
			if len(meth.Names) > 0 {
				fmt.Fprintf(&res, " ")
			}
			typ, newImps, err := identExprToGoName(constValues, modNames, file, meth.Type)
			if err != nil {
				return "", nil, err
			}
			imps = append(imps, newImps...)
			fmt.Fprintf(&res, "%v", typ)
		}
		fmt.Fprintf(&res, "}")
		return res.String(), imps, nil
	case *ast.StructType:
		var res strings.Builder
		var imps []*File
		fmt.Fprintf(&res, "struct{")
		for i, field := range expr.Fields.List {
			if i != 0 {
				fmt.Fprintf(&res, "; ")
			}
			for i, name := range field.Names {
				if i != 0 {
					fmt.Fprintf(&res, ", ")
				}
				fmt.Fprintf(&res, "%v", name.Name)
			}
			if len(field.Names) > 0 {
				fmt.Fprintf(&res, " ")
			}
			typ, newImps, err := identExprToGoName(constValues, modNames, file, field.Type)
			if err != nil {
				return "", nil, err
			}
			imps = append(imps, newImps...)
			fmt.Fprintf(&res, "%v", typ)
		}
		fmt.Fprintf(&res, "}")
		return res.String(), imps, nil
	case *ast.ChanType:
		val, imps, err := identExprToGoName(constValues, modNames, file, expr.Value)
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

func NewIdent(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, expr ast.Expr) (Ident, error) {
	name, imps, err := identExprToGoName(constValues, modNames, file, expr)
	if err != nil {
		return Ident{}, err
	}
	isEllipsis := false
	if _, ok := expr.(*ast.Ellipsis); ok {
		isEllipsis = true
	}
	return Ident{
		Expr:        expr,
		Name:        name,
		IsEllipsis:  isEllipsis,
		File:        file,
		UsedImports: imps,
	}, nil
}

// Returns *SelectorExpr referenced *File, true. If id is not *SelectorExpr, returns nil, false.
func (id Ident) GetReferencedPackage(modNames UniqueModuleNames, file *File) (*File, bool) {
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

// Name as func parameter
func (id Ident) ParamName() string {
	if !id.IsEllipsis {
		return id.Name
	}
	if !strings.HasPrefix(id.Name, "[]") {
		panic("expected ellipsis to be array type")
	}
	return "..." + id.Name[2:]
}

func (id Ident) RyeName() string {
	return "Go(" + id.Name + ")"
}

type Func struct {
	Name       Ident
	Recv       *Ident // non-nil for methods
	Params     []NamedIdent
	Results    []NamedIdent
	File       *File
	DocComment string
}

func NewFunc(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, fd *ast.FuncDecl) (*Func, error) {
	var err error
	res := &Func{
		File: file,
	}
	if fd.Recv == nil {
		res.Name, err = NewIdent(constValues, modNames, file, fd.Name)
		if err != nil {
			return nil, err
		}
	} else {
		res.Name, err = NewIdent(constValues, modNames, nil, fd.Name)
		if err != nil {
			return nil, err
		}
		if len(fd.Recv.List) != 1 {
			panic("expected exactly one receiver in method")
		}
		id, err := NewIdent(constValues, modNames, file, fd.Recv.List[0].Type)
		if err != nil {
			return nil, err
		}
		res.Recv = &id
	}
	fn := fd.Type
	{
		ids, _, err := ParamsToIdents(constValues, modNames, file, fn.Params)
		if err != nil {
			return nil, err
		}
		res.Params = ids
	}
	if fn.Results != nil {
		ids, _, err := ParamsToIdents(constValues, modNames, file, fn.Results)
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
		b.WriteString(fn.Recv.Name)
		b.WriteString(") ")
	}
	b.WriteString(fn.Name.Name)
	b.WriteString(" ")
	b.WriteString("(")
	for i, v := range fn.Params {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(v.Type.Name)
	}
	b.WriteString(") -> (")
	for i, v := range fn.Results {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(v.Type.Name)
	}
	b.WriteString(")")
	return b.String()
}

func ParamsToIdents(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, fl *ast.FieldList) (idents []NamedIdent, substImports []*File, err error) {
	var res []NamedIdent
	var substImps []*File
	for i, v := range fl.List {
		typID, err := NewIdent(constValues, modNames, file, v.Type)
		if err != nil {
			return nil, nil, err
		}
		substImps = append(substImps, typID.UsedImports...)
		if len(v.Names) > 0 {
			for _, n := range v.Names {
				nameID, err := NewIdent(constValues, modNames, nil, n)
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
			if typID.Name == "error" && i == len(fl.List)-1 {
				shorthand = "err"
			} else {
				shorthand = strconv.Itoa(i + 1)
			}
			nameID, err := NewIdent(constValues, modNames, nil, &ast.Ident{Name: shorthand})
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

func NewStruct(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, name *ast.Ident, structTyp *ast.StructType) (*Struct, error) {
	res := &Struct{
		Methods: make(map[string]*Func),
	}
	{
		id, err := NewIdent(constValues, modNames, file, name)
		if err != nil {
			return nil, err
		}
		res.Name = id
	}
	for _, f := range structTyp.Fields.List {
		if len(f.Names) > 0 {
			if !slices.ContainsFunc(f.Names, func(name *ast.Ident) bool {
				return name.IsExported()
			}) {
				// Don't even try to parse the field type if no name is exported
				continue
			}

			typID, err := NewIdent(constValues, modNames, file, f.Type)
			if err != nil {
				return nil, err
			}
			if IdentIsInternal(modNames, typID) {
				// Ignore field with internal type
				continue
			}

			for _, name := range f.Names {
				if !name.IsExported() {
					continue
				}
				nameID, err := NewIdent(constValues, modNames, nil, name)
				if err != nil {
					return nil, err
				}
				res.Fields = append(res.Fields, NamedIdent{
					Name: nameID,
					Type: typID,
				})
			}
		} else {
			structTyp := f.Type
			if se, ok := f.Type.(*ast.StarExpr); ok {
				structTyp = se.X
			}
			if !IdentExprIsExported(structTyp) {
				continue
			}
			structTypID, err := NewIdent(constValues, modNames, file, structTyp)
			if err != nil {
				return nil, err
			}
			res.Inherits = append(res.Inherits, structTypID)

			typID, err := NewIdent(constValues, modNames, file, f.Type)
			if err != nil {
				return nil, err
			}
			var nameExpr ast.Expr
			switch t := structTyp.(type) {
			case *ast.SelectorExpr:
				nameExpr = t.Sel
			case *ast.Ident:
				nameExpr = t
			default:
				return nil, fmt.Errorf("expected struct inheritance to be of type *ast.Ident or *ast.SelectorExpr")
			}
			nameID, err := NewIdent(constValues, modNames, nil, nameExpr)
			if err != nil {
				return nil, err
			}
			res.Fields = append(res.Fields, NamedIdent{
				Name: nameID,
				Type: typID,
			})
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

func funcFromInterfaceField(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, ifaceIdent Ident, f *ast.Field) (*Func, error) {
	var err error
	res := &Func{
		File: file,
	}
	if len(f.Names) != 1 {
		panic("expected interface method to have 1 name")
	}
	res.Name, err = NewIdent(constValues, modNames, nil, f.Names[0])
	if err != nil {
		return nil, err
	}
	res.Recv = &ifaceIdent
	fn, ok := f.Type.(*ast.FuncType)
	if !ok {
		panic("expected method type to be of type *ast.FuncType")
	}
	{
		ids, _, err := ParamsToIdents(constValues, modNames, file, fn.Params)
		if err != nil {
			return nil, err
		}
		res.Params = ids
	}
	if fn.Results != nil {
		ids, _, err := ParamsToIdents(constValues, modNames, file, fn.Results)
		if err != nil {
			return nil, err
		}
		res.Results = ids
	}
	return res, nil
}

func NewInterface(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, name *ast.Ident, ifaceTyp *ast.InterfaceType) (*Interface, error) {
	res := &Interface{}
	{
		id, err := NewIdent(constValues, modNames, file, name)
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
			fn, err := funcFromInterfaceField(constValues, modNames, file, res.Name, f)
			if err != nil {
				return nil, err
			}
			res.Funcs = append(res.Funcs, fn)
		case *ast.Ident, *ast.SelectorExpr:
			id, err := NewIdent(constValues, modNames, file, ft)
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
	res := fn.Name.Name
	if fn.Recv != nil {
		_, recvIsPtr := fn.Recv.Expr.(*ast.StarExpr)
		recv := fn.Recv.Name
		if recvIsPtr {
			recv = "(" + recv + ")"
		}
		res = recv + "." + res
	}
	return res
}

type ConstValue struct {
	ast.Expr
	File *File
	Iota int64
}

func EvalConstExpr(constValues map[string]ConstValue, modNames UniqueModuleNames, file *File, expr ast.Expr) (constant.Value, error) {
	makeVal := func(lit *ast.BasicLit) (constant.Value, error) {
		switch lit.Kind {
		case token.INT:
			v, err := strconv.ParseInt(lit.Value, 10, 64)
			if err != nil {
				return nil, err
			}
			return constant.MakeInt64(v), nil
		case token.FLOAT:
			v, err := strconv.ParseFloat(lit.Value, 64)
			if err != nil {
				return nil, err
			}
			return constant.MakeFloat64(v), nil
		case token.IMAG:
			v, err := strconv.ParseComplex(lit.Value, 128)
			if err != nil {
				return nil, err
			}
			return constant.BinaryOp(
				constant.MakeFloat64(real(v)),
				token.ADD,
				constant.MakeImag(constant.MakeFloat64(imag(v))),
			), nil
		case token.CHAR:
			v, _, _, err := strconv.UnquoteChar(lit.Value, 0)
			if err != nil {
				return nil, err
			}
			return constant.MakeInt64(int64(v)), nil
		case token.STRING:
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return nil, err
			}
			return constant.MakeString(v), nil
		default:
			panic("ast.BasicLit is of invalid type")
		}
	}

	var doEval func(file *File, expr ast.Expr, iotaVal int64) (constant.Value, error)
	doEval = func(file *File, expr ast.Expr, iotaVal int64) (constant.Value, error) {
		switch expr := expr.(type) {
		case *ast.SelectorExpr:
			switch parent := expr.X.(type) {
			case *ast.Ident:
				f, ok := file.ImportsByName[parent.Name]
				if !ok {
					return nil, fmt.Errorf("module %v imported by %v not found", parent.Name, file.Name)
				}
				return doEval(f, expr.Sel, iotaVal)
			default:
				return nil, fmt.Errorf("unexpected selector parent type %T", parent)
			}
		case *ast.Ident:
			if expr.Name == "iota" {
				if iotaVal < 0 {
					return nil, fmt.Errorf("unexpected iota")
				}
				return constant.MakeInt64(iotaVal), nil
			}
			mod, ok := modNames[file.ModulePath]
			if !ok {
				return nil, fmt.Errorf("unknown module path %v", file.ModulePath)
			}
			c, ok := constValues[mod+"."+expr.Name]
			if !ok {
				return nil, fmt.Errorf("reference to unknown const expr %v", mod+"."+expr.Name)
			}
			return doEval(c.File, c.Expr, c.Iota)
		case *ast.BasicLit:
			return makeVal(expr)
		case *ast.BinaryExpr:
			x, err := doEval(file, expr.X, iotaVal)
			if err != nil {
				return nil, err
			}

			y, err := doEval(file, expr.Y, iotaVal)
			if err != nil {
				return nil, err
			}

			return constant.BinaryOp(
				x,
				expr.Op,
				y,
			), nil
		default:
			return nil, fmt.Errorf("unexpected const expression type %T", expr)
		}
	}

	return doEval(file, expr, -1)
}

type IRInputFileInfo struct {
	File       *ast.File
	Name       string
	ModulePath string
	// only parse type declarations:
	// needed in case of inheritance dependency
	TypeDeclsOnly bool
}

type IR struct {
	Funcs       map[string]*Func
	Interfaces  map[string]*Interface
	Structs     map[string]*Struct
	Typedefs    map[string]Ident
	Values      map[string]NamedIdent // consts and vars
	Files       map[string]*File      // file by name
	ConstValues map[string]ConstValue
	TypeMethods map[string][]*Func // type to methods
}

// If a *multierror.Error is returned, that error is non-fatal and
// an IR was still generated.
func Parse(
	modNames UniqueModuleNames,
	modDefaultNames map[string]string,
	input []IRInputFileInfo,
	getDependency func(modulePath string) (map[string]*ast.File, error),
) (*IR, error) {
	var resErr error

	res := &IR{
		Funcs:       make(map[string]*Func),
		Interfaces:  make(map[string]*Interface),
		Structs:     make(map[string]*Struct),
		Typedefs:    make(map[string]Ident),
		Values:      make(map[string]NamedIdent),
		Files:       make(map[string]*File),
		ConstValues: make(map[string]ConstValue),
		TypeMethods: make(map[string][]*Func),
	}

	filesGoneThroughPrePass := make(map[string]struct{})
	filesGoneThroughMainPass := make(map[string]struct{})

	var addFiles func(input []IRInputFileInfo) error
	addFiles = func(input []IRInputFileInfo) error {
		var resErr error

		if len(input) == 0 {
			return nil
		}
		for _, in := range input {
			if _, ok := filesGoneThroughPrePass[in.Name]; ok {
				continue
			}
			if err := res.addFilePrePass(modNames, in.File, in.Name, in.ModulePath, modDefaultNames); err != nil {
				if multErr, ok := err.(*multierror.Error); ok {
					resErr = multierror.Append(resErr, multErr.Errors...)
				} else {
					return err
				}
			}
			filesGoneThroughPrePass[in.Name] = struct{}{}
		}
		newlyRequiredFiles := make(map[string]IRInputFileInfo)
		for _, in := range input {
			if _, ok := filesGoneThroughMainPass[in.Name]; ok {
				continue
			}
			newlyRequired, err := res.addFileMainPass(
				modNames,
				in.File, in.Name, in.TypeDeclsOnly,
			)
			if err != nil {
				if multErr, ok := err.(*multierror.Error); ok {
					resErr = multierror.Append(resErr, multErr.Errors...)
				} else {
					return err
				}
			}
			filesGoneThroughMainPass[in.Name] = struct{}{}

			for req := range newlyRequired {
				files, err := getDependency(req)
				if err != nil {
					return err
				}
				for name, file := range files {
					newlyRequiredFiles[name] = IRInputFileInfo{
						File:          file,
						Name:          name,
						ModulePath:    req,
						TypeDeclsOnly: true,
					}
				}
			}
		}

		// Do this after all the previous files, because these
		// are parsed with the TypeDeclsOnly flag. We don't want
		// to parse files intended to be fully parsed with
		// TypeDeclsOnly as filesGoneThroughMainPass would falsely
		// prevent them from being fully parsed after having been
		// parsed with TypeDeclsOnly.
		if err := addFiles(slices.Collect(maps.Values(newlyRequiredFiles))); err != nil {
			if multErr, ok := err.(*multierror.Error); ok {
				resErr = multierror.Append(resErr, multErr.Errors...)
			} else {
				return err
			}
		}

		return resErr
	}
	if err := addFiles(input); err != nil {
		return nil, err
	}

	if err := res.resolveInheritancesAndMethods(modNames); err != nil {
		return nil, err
	}

	return res, resErr
}

func (ir *IR) addFilePrePass(
	modNames UniqueModuleNames,
	f *ast.File,
	fName string,
	modulePath string,
	modDefaultNames map[string]string,
) error {
	var resErr error

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
					resErr = multierror.Append(resErr, fmt.Errorf("unable to get module name: unknown package %v (imported by %v)", path, modulePath))
					continue
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
	ir.Files[fName] = file

	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			if decl.Tok == token.CONST {
				var prevValue ast.Expr
				for specIdx, spec := range decl.Specs {
					if valSpec, ok := spec.(*ast.ValueSpec); ok {
						if len(valSpec.Names) != len(valSpec.Values) &&
							len(valSpec.Values) != 0 {
							return fmt.Errorf("expected const name count (%v) and value count (%v) to match", len(valSpec.Names), len(valSpec.Values))
						}
						for i := range valSpec.Names {
							mod, ok := modNames[modulePath]
							if !ok {
								panic("ir.Parse: expected modulePath to exist in modNames")
							}
							name := mod + "." + valSpec.Names[i].Name

							value := prevValue
							if i < len(valSpec.Values) && valSpec.Values[i] != nil {
								value = valSpec.Values[i]
							}
							prevValue = value

							if value == nil {
								return fmt.Errorf("expected value for const %v in module %v", name, mod)
							}

							ir.ConstValues[name] = ConstValue{
								Expr: value,
								File: file,
								Iota: int64(specIdx),
							}
						}
					}
				}
			}
		}
	}

	return resErr
}

func (ir *IR) addFileMainPass(
	modNames UniqueModuleNames,
	f *ast.File,
	fName string,
	// parse only type decls: needed for inheritance resolution
	typeDeclsOnly bool,
) (
	// packages needed for interface/struct inheritance resolution
	requiredPkgs map[string]struct{},
	resErr error,
) {
	requiredPkgs = make(map[string]struct{})

	file, ok := ir.Files[fName]
	if !ok {
		panic("main-pass: expected file " + fName + " to have been created in pre-pass")
	}

	docComments := make(map[token.Pos]string) // decl pos to comment text
	for _, comm := range f.Comments {
		docComments[comm.End()+1] = comm.Text()
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
			fn, err := NewFunc(ir.ConstValues, modNames, file, decl)
			if err != nil {
				resErr = multierror.Append(resErr, fmt.Errorf("parse %v: %w", file.ModuleName, err))
				continue
			}
			if fn.Recv != nil {
				ir.TypeMethods[fn.Recv.Name] = append(ir.TypeMethods[fn.Recv.Name], fn)
			}
			fn.DocComment = docComments[decl.Pos()]
			ir.Funcs[FuncGoIdent(fn)] = fn
		case *ast.GenDecl:
			if decl.Tok == token.CONST || decl.Tok == token.VAR {
				if typeDeclsOnly {
					continue
				}
				var typ *Ident
				for _, spec := range decl.Specs {
					if valSpec, ok := spec.(*ast.ValueSpec); ok {
						{ // Skip spec without exported name
							hasExported := false
							for _, name := range valSpec.Names {
								if name.IsExported() {
									hasExported = true
									break
								}
							}
							if !hasExported {
								continue
							}
						}
						if valSpec.Type != nil {
							newTyp, err := NewIdent(ir.ConstValues, modNames, file, valSpec.Type)
							if err != nil {
								resErr = multierror.Append(resErr, fmt.Errorf("const/var decl (names: %v): %w", valSpec.Names, err))
								continue declsLoop
							}
							typ = &newTyp
						} else if len(valSpec.Values) > 0 {
							if len(valSpec.Values) != 1 {
								panic("expected exactly 1 value in var/const value spec")
							}
							// deduce literal type from value
							typeName := ""
							switch expr := valSpec.Values[0].(type) {
							case *ast.BasicLit:
								switch expr.Kind {
								case token.STRING:
									typeName = "string"
								case token.INT:
									typeName = "int64"
								case token.FLOAT:
									typeName = "float64"
								}
							case *ast.Ident:
								if expr.Name == "iota" {
									typeName = "int64"
								}
							}
							if typeName != "" {
								ty, err := NewIdent(nil, nil, nil, &ast.Ident{Name: typeName})
								if err != nil {
									panic("unexpected error creating ident")
								}
								typ = &ty
							}
						}
						if typ == nil {
							continue
						}
						if len(valSpec.Names) != len(valSpec.Values) &&
							len(valSpec.Values) != 0 {
							return nil, fmt.Errorf("expected const/var name count (%v) and value count (%v) to match", len(valSpec.Names), len(valSpec.Values))
						}
						for i := range valSpec.Names {
							if !valSpec.Names[i].IsExported() {
								continue
							}
							name, err := NewIdent(ir.ConstValues, modNames, file, valSpec.Names[i])
							if err != nil {
								resErr = multierror.Append(resErr, fmt.Errorf("const/var decl (names: %v): %w", valSpec.Names, err))
								continue declsLoop
							}
							ir.Values[name.Name] = NamedIdent{
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
						iface, err := NewInterface(ir.ConstValues, modNames, file, typeSpec.Name, typ)
						if err != nil {
							return nil, err
						}
						ir.Interfaces[iface.Name.Name] = iface
						for _, id := range iface.Inherits {
							if refF, ok := id.GetReferencedPackage(modNames, iface.Name.File); ok {
								requiredPkgs[refF.ModulePath] = struct{}{}
							}
						}
					case *ast.StructType:
						struc, err := NewStruct(ir.ConstValues, modNames, file, typeSpec.Name, typ)
						if err != nil {
							resErr = multierror.Append(resErr, fmt.Errorf("struct decl for %v: %w", typeSpec.Name.Name, err))
							continue
						}
						ir.Structs[struc.Name.Name] = struc
						for _, id := range struc.Inherits {
							if refF, ok := id.GetReferencedPackage(modNames, struc.Name.File); ok {
								requiredPkgs[refF.ModulePath] = struct{}{}
							}
						}
					default:
						name, err := NewIdent(ir.ConstValues, modNames, file, typeSpec.Name)
						if err != nil {
							return nil, err
						}
						id, err := NewIdent(ir.ConstValues, modNames, file, typ)
						if err != nil {
							resErr = multierror.Append(resErr, fmt.Errorf("typedef for %v: %w", name.Name, err))
							continue
						}
						ir.Typedefs[name.Name] = id
					}
				}
			}
		}
	}
	return
}

// Resolves interface, struct, and method inheritance
func (ir *IR) resolveInheritancesAndMethods(modNames UniqueModuleNames) (resErr error) {
	var resolveInheritedIfaces func(iface *Interface) error
	resolveInheritedIfaces = func(iface *Interface) error {
		ifaceFnsEq := func(a, b *Func) bool {
			namedParamsEq := func(a, b NamedIdent) bool {
				return a.Type.Name == b.Type.Name
			}
			return slices.EqualFunc(a.Params, b.Params, namedParamsEq) &&
				slices.EqualFunc(a.Results, b.Results, namedParamsEq)
		}

		for _, inh := range iface.Inherits {
			inhIface, exists := ir.Interfaces[inh.Name]
			if !exists {
				resErr = multierror.Append(resErr, fmt.Errorf("cannot resolve interface inheritance %v in %v: does not exist", inh.Name, iface.Name.Name))
				continue
			}
			if err := resolveInheritedIfaces(inhIface); err != nil {
				return err
			}
			for _, fn := range inhIface.Funcs {
				// Check for duplicates
				isDuplicate := false
				for _, presentFn := range iface.Funcs {
					if presentFn.Name.Name == fn.Name.Name {
						if !ifaceFnsEq(presentFn, fn) {
							return errors.New("interface " + iface.Name.Name +
								" has conflicting methods, both named " + presentFn.Name.Name)
						}
						isDuplicate = true
					}
				}
				if isDuplicate {
					continue
				}

				fn := &Func{
					Name:    fn.Name,
					Recv:    &iface.Name,
					Params:  fn.Params,
					Results: fn.Results,
					File:    iface.Name.File,
				}
				iface.Funcs = append(iface.Funcs, fn)
			}
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
			recv, err = NewIdent(ir.ConstValues, modNames, fn.Recv.File, expr.X)
			if err != nil {
				return err
			}
		} else {
			recv = *fn.Recv
		}
		struc, ok := ir.Structs[recv.Name]
		if ok {
			struc.Methods[fn.Name.RyeName()] = fn
		}
	}

	var resolveInheritedStructs func(struc *Struct) error
	resolveInheritedStructs = func(struc *Struct) error {
		var methods []*Func
		numMethodNameOccurrences := make(map[string]int)
		var fields []NamedIdent
		numFieldNameOccurrences := make(map[string]int)
		toplevelExistingFieldNames := make(map[string]struct{})
		for _, inh := range struc.Inherits {
			if inhStruc, exists := ir.Structs[inh.Name]; exists {
				for name, fn := range inhStruc.Methods {
					methods = append(methods, fn)
					numMethodNameOccurrences[name]++
				}
				for _, field := range inhStruc.Fields {
					numFieldNameOccurrences[field.Name.Name]++
					fields = append(fields, field)
				}
			} else if _, exists := ir.Typedefs[inh.Name]; exists {
				for _, fn := range ir.TypeMethods[inh.Name] {
					methods = append(methods, fn)
					numMethodNameOccurrences[fn.Name.Name]++
				}
			} else {
				return errors.New("struct inheritance " + inh.Name + " from " + inh.File.ModulePath + " is unknown")
			}
		}
		for _, field := range struc.Fields {
			toplevelExistingFieldNames[field.Name.Name] = struct{}{}
		}
		for _, inh := range struc.Inherits {
			if inhStruc, exists := ir.Structs[inh.Name]; exists {
				if err := resolveInheritedStructs(inhStruc); err != nil {
					return err
				}
			}
		}
		struc.Inherits = nil
		for _, meth := range methods {
			name := meth.Name.Name
			if numMethodNameOccurrences[name] > 1 {
				// Skip ambiguous method selectors
				continue
			}
			if _, ok := toplevelExistingFieldNames[name]; ok {
				// Local fields override inherited methods
				continue
			}
			if _, exists := struc.Methods[name]; exists {
				continue
			}
			m := &Func{
				Name:    meth.Name,
				Recv:    &struc.Name,
				Params:  slices.Clone(meth.Params),
				Results: slices.Clone(meth.Results),
				File:    struc.Name.File,
			}

			if _, ok := meth.Recv.Expr.(*ast.StarExpr); ok {
				recv, err := NewIdent(ir.ConstValues, modNames, struc.Name.File, &ast.StarExpr{X: struc.Name.Expr})
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
		for _, field := range fields {
			name := field.Name.Name
			if numFieldNameOccurrences[name] > 1 {
				// Skip ambiguous field selectors
				continue
			}
			if _, ok := toplevelExistingFieldNames[name]; ok {
				continue
			}
			struc.Fields = append(struc.Fields, field)
		}
		return nil
	}
	for _, struc := range ir.Structs {
		if err := resolveInheritedStructs(struc); err != nil {
			return err
		}
	}

	return resErr
}
