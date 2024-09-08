package binder

import (
	"errors"
	"fmt"
	"go/ast"
	"strconv"
	"strings"

	"github.com/refaktor/ryegen/binder/binderio"
	"github.com/refaktor/ryegen/ir"
)

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

type BindingFuncID struct {
	Recv      string
	Name      string
	NameIdent *ir.Ident
}

func (id BindingFuncID) FullName() string {
	if id.Recv != "" {
		return id.Recv + "//" + id.Name
	} else {
		return id.Name
	}
}

func (id BindingFuncID) SplitGoNameAndMod() (string, *ir.File) {
	file := id.NameIdent.File
	var name string
	switch expr := id.NameIdent.Expr.(type) {
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

type BindingFunc struct {
	BindingFuncID
	Doc   string
	Argsn int
	Body  string
}

func GenerateBinding(deps *Dependencies, ctx *Context, fn *ir.Func) (*BindingFunc, error) {
	res := &BindingFunc{}
	res.Name = fn.Name.RyeName
	res.NameIdent = &fn.Name
	if fn.Recv != nil {
		res.Recv = fn.Recv.RyeName
	}

	var cb binderio.CodeBuilder

	params := fn.Params
	if fn.Recv != nil {
		recvName, _ := ir.NewIdent(ctx.ModNames, nil, &ast.Ident{Name: "__recv"})
		params = append([]ir.NamedIdent{{Name: recvName, Type: *fn.Recv}}, params...)
	}

	if len(params) > 5 {
		return nil, errors.New("can only handle at most 5 parameters")
	}

	res.Doc = ir.FuncGoIdent(fn)
	res.Argsn = len(params)

	derefParam := make([]bool, len(params))

	for i, param := range params {
		typ := param.Type
		if _, ok := ctx.IR.Structs[typ.GoName]; ok {
			var err error
			typ, err = ir.NewIdent(ctx.ModNames, typ.File, &ast.StarExpr{X: typ.Expr})
			if err != nil {
				panic(err)
			}
			derefParam[i] = true
		}
		cb.Linef(`var arg%vVal %v`, i, typ.GoName)
		deps.MarkUsed(typ)
		if _, found := ConvRyeToGo(
			deps,
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

	resultsWithoutErr := fn.Results
	var errResult *ir.NamedIdent
	if len(fn.Results) > 0 && fn.Results[len(fn.Results)-1].Type.GoName == "error" {
		resultsWithoutErr = fn.Results[:len(fn.Results)-1]
		errResult = &fn.Results[len(fn.Results)-1]
	}

	resultIdxName := func(i int) string {
		if errResult != nil && i == len(fn.Results)-1 {
			return "Err"
		}
		return strconv.Itoa(i)
	}

	var assign strings.Builder
	{
		for i := range fn.Results {
			if i != 0 {
				assign.WriteString(`, `)
			}
			assign.WriteString(fmt.Sprintf(`res%v`, resultIdxName(i)))
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
	deps.MarkUsed(fn.Name)

	for i, result := range fn.Results {
		addr := ""
		typ := result.Type
		if _, ok := ctx.IR.Structs[typ.GoName]; ok {
			var err error
			typ, err = ir.NewIdent(ctx.ModNames, typ.File, &ast.StarExpr{X: typ.Expr})
			if err != nil {
				panic(err)
			}
			addr = "&"
		}
		cb.Linef(`var res%vObj env.Object`, resultIdxName(i))
		if _, found := ConvGoToRye(
			deps,
			ctx,
			&cb,
			typ,
			fmt.Sprintf(`res%vObj`, resultIdxName(i)),
			fmt.Sprintf(`%vres%v`, addr, resultIdxName(i)),
			-1,
			nil,
		); !found {
			return nil, errors.New("unhandled type conversion (go to rye): " + result.Type.GoName)
		}
	}
	if errResult != nil {
		cb.Linef(`if resErrObj != nil {`)
		cb.Indent++
		cb.Linef(`ps.FailureFlag = true`)
		cb.Linef(`return resErrObj`)
		cb.Indent--
		cb.Linef(`}`)
	}
	if len(resultsWithoutErr) > 0 {
		if len(resultsWithoutErr) == 1 {
			cb.Linef(`return res0Obj`)
		} else {
			cb.Linef(`return *env.NewBlock(*env.NewTSeries([]env.Object{`)
			cb.Indent++
			for i := range resultsWithoutErr {
				cb.Linef(`res%vObj,`, i)
			}
			cb.Indent--
			cb.Linef(`}))`)
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

func GenerateGetterOrSetter(deps *Dependencies, ctx *Context, field ir.NamedIdent, structName ir.Ident, setter bool) (*BindingFunc, error) {
	res := &BindingFunc{}

	{
		var err error
		structName, err = ir.NewIdent(ctx.ModNames, structName.File, &ast.StarExpr{X: structName.Expr})
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

	var cb binderio.CodeBuilder

	if setter {
		res.Doc = fmt.Sprintf("Set %v %v value", structName.GoName, field.Name.GoName)
		res.Argsn = 2
	} else {
		res.Doc = fmt.Sprintf("Get %v %v value", structName.GoName, field.Name.GoName)
		res.Argsn = 1
	}

	cb.Linef(`var self %v`, structName.GoName)
	deps.MarkUsed(structName)
	if _, found := ConvRyeToGo(
		deps,
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
			deps,
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
		if _, ok := ctx.IR.Structs[typ.GoName]; ok {
			var err error
			typ, err = ir.NewIdent(ctx.ModNames, typ.File, &ast.StarExpr{X: typ.Expr})
			if err != nil {
				panic(err)
			}
			addr = "&"
		}
		cb.Linef(`var resObj env.Object`)
		if _, found := ConvGoToRye(
			deps,
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

func GenerateValue(deps *Dependencies, ctx *Context, value ir.NamedIdent) (*BindingFunc, error) {
	res := &BindingFunc{}
	res.Name = value.Name.RyeName
	res.NameIdent = &value.Name
	res.Doc = fmt.Sprintf("Get %v value", value.Name.GoName)
	res.Argsn = 0

	deps.MarkUsed(value.Name)

	var cb binderio.CodeBuilder

	cb.Linef(`var resObj env.Object`)
	if _, found := ConvGoToRye(
		deps,
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

func GenerateNewStruct(deps *Dependencies, ctx *Context, structName ir.Ident) (*BindingFunc, error) {
	res := &BindingFunc{}
	{
		id, err := ir.NewIdent(ctx.ModNames, structName.File, &ast.Ident{Name: "New" + structName.Expr.(*ast.Ident).Name})
		if err != nil {
			return nil, err
		}
		res.Name = id.RyeName
		res.NameIdent = &id
	}
	res.Doc = fmt.Sprintf("Create a new %v struct", structName.GoName)
	res.Argsn = 0

	deps.MarkUsed(structName)

	structPtr, err := ir.NewIdent(ctx.ModNames, structName.File, &ast.StarExpr{X: structName.Expr})
	if err != nil {
		panic(err)
	}

	var cb binderio.CodeBuilder
	cb.Linef(`res := &%v{}`, structName.GoName)
	cb.Linef(`var resObj env.Object`)
	if _, found := ConvGoToRye(
		deps,
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

func GenerateGenericInterfaceImpl(deps *Dependencies, ctx *Context, iface *ir.Interface) (string, error) {
	var cb binderio.CodeBuilder

	name := "iface_" + strings.ReplaceAll(iface.Name.GoName, ".", "_")
	cb.Linef(`type %v struct {`, name)
	cb.Indent++
	cb.Linef(`self env.RyeCtx`)
	makeFnTyp := func(fn *ir.Func, withSelf, selfAsRecv bool) string {
		var s strings.Builder
		s.WriteString("func")
		if withSelf && selfAsRecv {
			s.WriteString(fmt.Sprintf(" (self *%v) %v", name, fn.Name.GoName))
		}
		s.WriteString("(")
		nParamsW := 0
		if withSelf && !selfAsRecv {
			s.WriteString("self env.RyeCtx")
			nParamsW++
		}
		for i, param := range fn.Params {
			if nParamsW != 0 {
				s.WriteString(", ")
			}
			s.WriteString(fmt.Sprintf("arg%v %v", i, param.Type.GoName))
			deps.MarkUsed(param.Type)
			nParamsW++
		}
		s.WriteString(")")
		if len(fn.Results) > 0 {
			s.WriteString(" (")
			for i, result := range fn.Results {
				if i != 0 {
					s.WriteString(", ")
				}
				s.WriteString(result.Type.GoName)
				deps.MarkUsed(result.Type)
			}
			s.WriteString(")")
		}
		return s.String()
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
	deps.MarkUsed(iface.Name)
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
		deps.Imports["errors"] = struct{}{}
		cb.Indent--
		cb.Linef(`}`)
		if !ConvRyeToGoCodeFunc(
			deps,
			ctx,
			&cb,
			fmt.Sprintf(`impl.fn_%v`, fn.Name.GoName),
			fmt.Sprintf(`ctxObj%v`, i),
			false,
			-1,
			func(inner string) string {
				deps.Imports["errors"] = struct{}{}
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
