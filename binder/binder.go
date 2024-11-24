package binder

import (
	"errors"
	"fmt"
	"go/ast"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/refaktor/ryegen/binder/binderio"
	"github.com/refaktor/ryegen/ir"
)

func makeMakeRetArgErr(argn int) func(inner string) string {
	return func(inner string) string {
		var cb binderio.CodeBuilder
		cb.Linef(`ps.FailureFlag = true`)
		cb.Linef(`return env.NewError("((RYEGEN:FUNCNAME)): arg %v: "+%v)`, argn+1, inner)
		return cb.String()
	}
}

type BindingFuncID struct {
	Recv     string
	Name     string
	Category string // for collecting stats
	File     *ir.File
}

func (id BindingFuncID) modPrefix(ctx *Context) string {
	if id.Recv == "" {
		prefix := ctx.ModNames[id.File.ModulePath]
		if len(prefix) < 1 {
			panic("expected module with valid name")
		}
		prefix = strings.ToUpper(prefix[:1]) + prefix[1:]
		return prefix
	}
	return ""
}

func (id BindingFuncID) UniqueName(ctx *Context) string {
	prefix := id.modPrefix(ctx)
	if id.Recv != "" {
		return id.Recv + "//" + strcase.ToKebab(id.Name)
	} else {
		return strcase.ToKebab(prefix + id.Name)
	}
}

// Returned descending by priority
// renameCandidate (optional) has top priority
func (id BindingFuncID) RyeifiedNameCandidates(ctx *Context, noPrefix, cutNew bool, renameCandidate string) (candidates []string) {
	prefix := id.modPrefix(ctx)

	addCandidate := func(s string) {
		if id.Recv != "" {
			candidates = append(candidates, id.Recv+"//"+strcase.ToKebab(s))
		} else {
			candidates = append(candidates, strcase.ToKebab(s))
		}
	}

	// Add custom rename candidate
	if renameCandidate != "" {
		addCandidate(renameCandidate)
	}

	newWasCut := false
	if cutNew {
		if after, found := strings.CutPrefix(id.Name, "New"); found {
			if after == "" {
				if noPrefix {
					// e.g. app.New => app
					addCandidate(prefix)
				}
				// e.g. app.New => app-app
				addCandidate(prefix + prefix)
			} else {
				if noPrefix {
					// e.g. lib.NewApp => app
					addCandidate(after)
				}
				// e.g. lib.NewApp => lib-app
				addCandidate(prefix + after)
			}
			newWasCut = true
		}
	}
	if !newWasCut {
		if noPrefix {
			// e.g. lib.NewApp => new-app
			addCandidate(id.Name)
		}
		// e.g. lib.NewApp => lib-new-app
		addCandidate(prefix + id.Name)
	}

	return candidates
}

type BindingFunc struct {
	BindingFuncID
	Doc        string
	DocComment string
	Argsn      int
	Body       string
}

func GenerateBinding(deps *Dependencies, ctx *Context, fn *ir.Func) (*BindingFunc, error) {
	res := &BindingFunc{}

	var docComment strings.Builder
	docComment.WriteString(fn.DocComment)
	if fn.DocComment != "" {
		docComment.WriteString("\n")
	}
	if fn.Recv != nil || len(fn.Params) > 0 {
		docComment.WriteString("## Parameters\n")
		if fn.Recv != nil {
			typName, err := GetRyeTypeDesc(ctx, fn.Recv.File, fn.Recv.Expr)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&docComment, "recv: %v\n", typName)
		}
		for _, param := range fn.Params {
			typName, err := GetRyeTypeDesc(ctx, param.Type.File, param.Type.Expr)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&docComment, "%v: %v\n", strcase.ToKebab(param.Name.Name), typName)
		}
	}
	{
		results := fn.Results
		canErr := false
		if len(results) > 0 {
			if results[len(results)-1].Type.Name == "error" {
				results = results[:len(results)-1]
				canErr = true
			}
			docComment.WriteString("## Result\n")
			if len(results) == 1 {
				typName, err := GetRyeTypeDesc(ctx, results[0].Type.File, results[0].Type.Expr)
				if err != nil {
					return nil, err
				}
				fmt.Fprintf(&docComment, "%v\n", typName)
			} else if len(results) > 1 {
				docComment.WriteString("[\n")
				for _, param := range results {
					typName, err := GetRyeTypeDesc(ctx, param.Type.File, param.Type.Expr)
					if err != nil {
						return nil, err
					}
					fmt.Fprintf(&docComment, "    %v\n", typName)
				}
				docComment.WriteString("]\n")
			}
			if canErr {
				docComment.WriteString("+ can-error\n")
			}
		}
	}

	res.DocComment = docComment.String()

	if fn.Recv == nil {
		res.Category = "Functions"
	} else {
		res.Category = "Methods"
	}

	{
		id, ok := fn.Name.Expr.(*ast.Ident)
		if !ok {
			panic("expected func name to be *ast.Ident")
		}
		res.Name = id.Name
	}
	res.File = fn.File

	if fn.Recv != nil {
		typ := *fn.Recv
		if _, ok := ctx.IR.Structs[typ.Name]; ok {
			var err error
			typ, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, &ast.StarExpr{X: typ.Expr})
			if err != nil {
				panic(err)
			}
		}
		res.Recv = typ.RyeName()
	}

	var cb binderio.CodeBuilder

	res.Doc = ir.FuncGoIdent(fn)
	res.Argsn = len(fn.Params)
	if fn.Recv != nil {
		res.Argsn++
	}

	if err := ConvGoToRyeCodeFuncBody(
		deps,
		ctx,
		&cb,
		fn.Name.Name,
		makeMakeRetArgErr(1),
		fn.Recv,
		fn.Params,
		fn.Results,
	); err != nil {
		return nil, err
	}
	deps.Imports[fn.File.ModulePath] = struct{}{}

	res.Body = cb.String()

	return res, nil
}

func GenerateGetterOrSetter(deps *Dependencies, ctx *Context, field ir.NamedIdent, structName ir.Ident, setter bool) (*BindingFunc, error) {
	res := &BindingFunc{}
	if setter {
		res.Category = "Setters"
	} else {
		res.Category = "Getters"
	}

	var docComment strings.Builder
	if setter {
		docComment.WriteString("## Parameters\n")
		typName, err := GetRyeTypeDesc(ctx, field.Type.File, field.Type.Expr)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&docComment, "%v: %v\n", strcase.ToKebab(field.Name.Name), typName)
	}
	docComment.WriteString("## Result\n")
	typName, err := GetRyeTypeDesc(ctx, field.Type.File, field.Type.Expr)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&docComment, "%v\n", typName)
	res.DocComment = docComment.String()

	{
		var err error
		structName, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, structName.File, &ast.StarExpr{X: structName.Expr})
		if err != nil {
			return nil, err
		}
	}

	res.Recv = structName.RyeName()
	if setter {
		res.Name = field.Name.Name + "!"
	} else {
		res.Name = field.Name.Name + "?"
	}
	res.File = structName.File

	var cb binderio.CodeBuilder

	if setter {
		res.Doc = fmt.Sprintf("Set %v %v value", structName.Name, field.Name.Name)
		res.Argsn = 2
	} else {
		res.Doc = fmt.Sprintf("Get %v %v value", structName.Name, field.Name.Name)
		res.Argsn = 1
	}

	cb.Linef(`var self %v`, structName.Name)
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
		return nil, errors.New("unhandled type conversion (go to rye): " + structName.Name)
	}

	typIsNonPtrStruct := false
	ptrTyp := field.Type
	if _, ok := ctx.IR.Structs[ptrTyp.Name]; ok {
		var err error
		ptrTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, ptrTyp.File, &ast.StarExpr{X: ptrTyp.Expr})
		if err != nil {
			panic(err)
		}
		typIsNonPtrStruct = true
	}

	if setter {
		cb.Linef(`var newVal %v`, ptrTyp.Name)
		deps.MarkUsed(ptrTyp)
		if _, found := ConvRyeToGo(
			deps,
			ctx,
			&cb,
			ptrTyp,
			`newVal`,
			`arg1`,
			1,
			makeMakeRetArgErr(1),
		); !found {
			return nil, errors.New("unhandled type conversion (go to rye): " + structName.Name)
		}

		deref := ""
		if typIsNonPtrStruct {
			deref = "*"
		}
		cb.Linef(`self.%v = %vnewVal`, field.Name.Name, deref)

		cb.Linef(`return arg0`)
	} else {
		addr := ""
		if typIsNonPtrStruct {
			addr = "&"
		}
		cb.Linef(`var resObj env.Object`)
		if _, found := ConvGoToRye(
			deps,
			ctx,
			&cb,
			ptrTyp,
			`resObj`,
			addr+`self.`+field.Name.Name,
			-1,
			nil,
		); !found {
			return nil, errors.New("unhandled type conversion (go to rye): " + field.Type.Name)
		}
		cb.Linef(`return resObj`)
	}
	res.Body = cb.String()

	return res, nil
}

func GenerateValue(deps *Dependencies, ctx *Context, value ir.NamedIdent) (*BindingFunc, error) {
	res := &BindingFunc{}

	res.Category = "Global vars/consts"

	{
		id, ok := value.Name.Expr.(*ast.Ident)
		if !ok {
			panic("expected var/const name to be *ast.Ident")
		}
		res.Name = id.Name
	}

	var docComment strings.Builder
	docComment.WriteString("## Result\n")
	typName, err := GetRyeTypeDesc(ctx, value.Type.File, value.Type.Expr)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&docComment, "%v\n", typName)
	res.DocComment = docComment.String()

	res.File = value.Name.File
	res.Doc = fmt.Sprintf("Get %v value", value.Name.Name)
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
		value.Name.Name,
		-1,
		nil,
	); !found {
		return nil, errors.New("unhandled type conversion (go to rye): " + value.Type.Name)
	}
	cb.Linef(`return resObj`)
	res.Body = cb.String()

	return res, nil
}

func GenerateNewStruct(deps *Dependencies, ctx *Context, structName ir.Ident) (*BindingFunc, error) {
	res := &BindingFunc{}
	res.Category = "Struct initializers"
	{
		id, ok := structName.Expr.(*ast.Ident)
		if !ok {
			panic("expected var/const name to be *ast.Ident")
		}
		res.Name = "New" + id.Name
	}
	res.File = structName.File
	res.Doc = fmt.Sprintf("Create a new %v struct", structName.Name)
	res.Argsn = 0

	deps.MarkUsed(structName)

	structPtr, err := ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, structName.File, &ast.StarExpr{X: structName.Expr})
	if err != nil {
		panic(err)
	}

	var cb binderio.CodeBuilder
	cb.Linef(`res := &%v{}`, structName.Name)
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
		return nil, errors.New("unhandled type conversion (go to rye): " + structName.Name)
	}
	cb.Linef(`return resObj`)
	res.Body = cb.String()

	return res, nil
}

func GenerateGenericInterfaceImpl(deps *Dependencies, ctx *Context, iface *ir.Interface) (string, error) {
	var cb binderio.CodeBuilder

	name := "iface_" + strings.ReplaceAll(iface.Name.Name, ".", "_")
	cb.Linef(`type %v struct {`, name)
	cb.Indent++
	cb.Linef(`self env.RyeCtx`)
	makeFnTyp := func(fn *ir.Func, withSelf, selfAsRecv bool) string {
		var s strings.Builder
		s.WriteString("func")
		if withSelf && selfAsRecv {
			s.WriteString(fmt.Sprintf(" (self *%v) %v", name, fn.Name.Name))
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
			s.WriteString(fmt.Sprintf("arg%v %v", i, param.Type.ParamName()))
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
				s.WriteString(result.Type.Name)
				deps.MarkUsed(result.Type)
			}
			s.WriteString(")")
		}
		return s.String()
	}
	for _, fn := range iface.Funcs {
		cb.Linef(`fn_%v %v`, fn.Name.Name, makeFnTyp(fn, true, false))
	}
	cb.Indent--
	cb.Linef(`}`)
	for _, fn := range iface.Funcs {
		cb.Linef(``)
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
		cb.Linef(`%vself.fn_%v(%v)`, retStmt, fn.Name.Name, argsB.String())
		cb.Indent--
		cb.Linef(`}`)
	}
	cb.Linef(``)

	cb.Linef(`func ctxTo_%v(ps *env.ProgramState, v env.RyeCtx) (%v, error) {`, strings.ReplaceAll(iface.Name.Name, ".", "_"), iface.Name.Name)
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
	implTyp := "iface_" + strings.ReplaceAll(iface.Name.Name, ".", "_")
	cb.Linef(`impl := &%v{`, implTyp)
	cb.Indent++
	cb.Linef(`self: v,`)
	cb.Indent--
	cb.Linef(`}`)
	for i, fn := range iface.Funcs {
		cb.Linef(`ctxObj%v, ok := wordToObj["%v"]`, i, strcase.ToKebab(fn.Name.Name))
		cb.Linef(`if !ok {`)
		cb.Indent++
		cb.Linef(`return nil, errors.New("context to %v: expected context to have function %v")`, iface.Name.Name, fn.Name.Name)
		deps.Imports["errors"] = struct{}{}
		cb.Indent--
		cb.Linef(`}`)
		if !ConvRyeToGoCodeFunc(
			deps,
			ctx,
			&cb,
			fmt.Sprintf(`impl.fn_%v`, fn.Name.Name),
			fmt.Sprintf(`ctxObj%v`, i),
			false,
			-1,
			func(inner string) string {
				deps.Imports["errors"] = struct{}{}
				return fmt.Sprintf(`return nil, errors.New("context to %v: context fn %v: "+%v)`, iface.Name.Name, fn.Name.Name, inner)
			},
			true,
			fn.Params,
			fn.Results,
		) {
			return "", errors.New("unhandled function conversion (rye to go): " + fn.Name.Name)
		}
	}
	cb.Linef(`return impl, nil`)
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(``)

	return cb.String(), nil
}
