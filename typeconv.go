package ryegen

import (
	"fmt"
	"go/ast"
	"strings"
)

type Converter struct {
	Name    string
	TryConv func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool
}

func ConvRyeToGo(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) (string, bool) {
	for _, conv := range ConvListRyeToGo {
		if conv.TryConv(ctx, cb, typ, outVar, inVar, argn, makeRetConvErr) {
			return conv.Name, true
		}
	}
	return "", false
}

func ConvGoToRye(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) (string, bool) {
	for _, conv := range ConvListGoToRye {
		if conv.TryConv(ctx, cb, typ, outVar, inVar, argn, makeRetConvErr) {
			return conv.Name, true
		}
	}
	return "", false
}

func getUnderlyingType(ctx *Context, typ Ident) (Ident, bool) {
	retOk := false
	for {
		if underlying, ok := ctx.Data.Typedefs[typ.GoName]; ok {
			retOk = true
			typ = underlying
		} else {
			break
		}
	}
	return typ, retOk
}

// If conversion lists are declared directly, the compiler falsely complains of an initialization cycle.
var ConvListRyeToGo []Converter
var ConvListGoToRye []Converter

func init() {
	ConvListRyeToGo = convListRyeToGo
	ConvListGoToRye = convListGoToRye
}

func convRyeToGoCodeCaseNative(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) {
	cb.Linef(`case env.Native:`)
	cb.Indent++
	cb.Linef(`var ok bool`)
	cb.Linef(`%v, ok = %v.Value.(%v)`, outVar, inVar, typ.GoName)
	ctx.MarkUsed(typ)
	cb.Linef(`if !ok {`)
	cb.Indent++
	cb.Append(makeRetConvErr(fmt.Sprintf("expected native of type %v", typ.GoName)))
	cb.Indent--
	cb.Linef(`}`)
	cb.Indent--
}

func convRyeToGoCodeCaseNil(cb *CodeBuilder, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) {
	cb.Linef(`case env.Integer:`)
	cb.Indent++
	cb.Linef(`if %v.Value != 0 {`, inVar)
	cb.Indent++
	cb.Append(makeRetConvErr(fmt.Sprintf("expected integer to be 0 or nil")))
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`%v = nil`, outVar)
	cb.Indent--
}

func ConvRyeToGoCodeFunc(ctx *Context, cb *CodeBuilder, outVar, inVar string, argn int, makeRetConvErr func(inner string) string, recv bool, params, results []NamedIdent) bool {
	var fnTyp string
	{
		var fnTypB strings.Builder
		fnTypB.WriteString("func(")
		nParamsWritten := 0
		if recv {
			fnTypB.WriteString("ctx env.RyeCtx")
			nParamsWritten++
		}
		for i, param := range params {
			if nParamsWritten != 0 {
				fnTypB.WriteString(", ")
			}
			fnTypB.WriteString(fmt.Sprintf("arg%v %v", i, param.Type.GoName))
			ctx.MarkUsed(param.Type)
			nParamsWritten++
		}
		fnTypB.WriteString(")")
		if len(results) > 0 {
			fnTypB.WriteString(" (")
			for i, result := range results {
				if i != 0 {
					fnTypB.WriteString(", ")
				}
				fnTypB.WriteString(result.Type.GoName)
				ctx.MarkUsed(result.Type)
			}
			fnTypB.WriteString(")")
		}
		fnTyp = fnTypB.String()
	}

	cb.Linef(`switch fn := %v.(type) {`, inVar)
	cb.Linef(`case env.Function:`)
	cb.Indent++
	cb.Linef(`if fn.Argsn != %v {`, len(params))
	cb.Indent++
	cb.Append(makeRetConvErr(fmt.Sprintf("function has invalid number of arguments (expected %v)", len(params))))
	cb.Indent--
	cb.Linef(`}`)

	cb.Linef(`%v = %v {`, outVar, fnTyp)
	cb.Indent++
	var argVals strings.Builder
	for i := range params {
		if i != 0 {
			argVals.WriteString(", ")
		}
		argVals.WriteString(fmt.Sprintf("arg%vVal", i))
	}
	if len(params) > 0 {
		cb.Linef(`var %v env.Object`, argVals.String())
	}
	for i, param := range params {
		if _, found := ConvGoToRye(
			ctx,
			cb,
			param.Type,
			fmt.Sprintf(`arg%vVal`, i),
			fmt.Sprintf(`arg%v`, i),
			argn,
			nil,
		); !found {
			return false
		}
	}
	var retStmt string
	{
		if len(results) == 0 {
			retStmt = "return"
		} else if len(results) == 1 {
			retStmt = "return res"
		} else {
			var retB strings.Builder
			fmt.Fprintf(&retB, "return ")
			for i := range results {
				if i != 0 {
					fmt.Fprintf(&retB, ", ")
				}
				fmt.Fprintf(&retB, "res%v", i)
			}
			retStmt = retB.String()
		}
	}
	// required for nested functions to work, since the inner "fn" function value
	// might be an integer or a native
	cb.Linef(`actualFn := fn`)
	cb.Linef(`_ = actualFn`)
	makeFnResultRetConvErr := func(inner string) string {
		ctx.UsedImports["fmt"] = struct{}{}
		ctx.UsedImports["errors"] = struct{}{}
		return fmt.Sprintf(`fmt.Printf("\033[31mError: \033[1m%%v\033[m\n\033[31mFrom function \033[1m%%v { %%v }\033[m\n",
	"((RYEGEN:FUNCNAME)): arg %v: callback result: %v",
	actualFn.Spec.Series.PositionAndSurroundingElements(*ps.Idx),
	actualFn.Body.Series.PositionAndSurroundingElements(*ps.Idx),
)
%v
`, argn+1, inner, retStmt)
	}
	ctxIdent := "ps.Ctx"
	if recv {
		ctxIdent = "&ctx"
	}
	argValsComma := ""
	if len(params) > 0 {
		argValsComma = ", "
	}
	cb.Linef(`evaldo.CallFunctionArgsN(fn, ps, %v%v%v)`, ctxIdent, argValsComma, argVals.String())
	if len(results) == 1 {
		cb.Linef(`var res %v`, results[0].Type.GoName)
		ctx.MarkUsed(results[0].Type)
		if _, found := ConvRyeToGo(
			ctx,
			cb,
			results[0].Type,
			`res`,
			`ps.Res`,
			argn,
			makeFnResultRetConvErr,
		); !found {
			return false
		}
		cb.Linef(`%v`, retStmt)
	} else if len(results) > 1 {
		for i, res := range results {
			cb.Linef(`var res%v %v`, i, res.Type.GoName)
		}
		cb.Linef(`res, ok := ps.Res.(env.Block)`)
		cb.Linef(`if !ok {`)
		cb.Indent++
		cb.Append(makeFnResultRetConvErr("expected block for multiple return values"))
		cb.Indent--
		cb.Linef(`}`)
		cb.Linef(`if len(res.Series.S) != %v {`, len(results))
		cb.Indent++
		cb.Append(makeFnResultRetConvErr(fmt.Sprintf("expected block with %v return values", len(results))))
		cb.Indent--
		cb.Linef(`}`)
		for i, res := range results {
			ctx.MarkUsed(res.Type)
			if _, found := ConvRyeToGo(
				ctx,
				cb,
				res.Type,
				fmt.Sprintf(`res%v`, i),
				fmt.Sprintf(`res.Series.S[%v]`, i),
				argn,
				makeFnResultRetConvErr,
			); !found {
				return false
			}
		}
		cb.Linef(`%v`, retStmt)
	}
	cb.Indent--
	cb.Linef(`}`)
	cb.Indent--
	convRyeToGoCodeCaseNil(cb, outVar, `fn`, argn, makeRetConvErr)
	cb.Linef(`default:`)
	cb.Indent++
	cb.Append(makeRetConvErr(fmt.Sprintf("expected function or nil")))
	cb.Indent--
	cb.Linef(`}`)
	return true
}

var convListRyeToGo = []Converter{
	{
		Name: "array",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var elTyp Ident
			switch t := typ.Expr.(type) {
			case *ast.ArrayType:
				var err error
				elTyp, err = NewIdent(ctx, typ.File, t.Elt)
				if err != nil {
					// TODO
					panic(err)
				}
			case *ast.Ellipsis:
				var err error
				elTyp, err = NewIdent(ctx, typ.File, t.Elt)
				if err != nil {
					// TODO
					panic(err)
				}
			default:
				return false
			}

			cb.Linef(`switch v := %v.(type) {`, inVar)
			cb.Linef(`case env.Block:`)
			cb.Indent++
			cb.Linef(`%v = make(%v, len(v.Series.S))`, outVar, typ.GoName)
			ctx.MarkUsed(typ)
			cb.Linef(`for i, it := range v.Series.S {`)
			cb.Indent++
			if _, found := ConvRyeToGo(
				ctx,
				cb,
				elTyp,
				fmt.Sprintf(`%v[i]`, outVar),
				`it`,
				argn,
				func(inner string) string {
					return makeRetConvErr("block item: " + inner)
				},
			); !found {
				return false
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Indent--
			convRyeToGoCodeCaseNative(ctx, cb, typ, outVar, `v`, argn, makeRetConvErr)
			convRyeToGoCodeCaseNil(cb, outVar, `v`, argn, makeRetConvErr)
			cb.Linef(`default:`)
			cb.Indent++
			cb.Append(makeRetConvErr(fmt.Sprintf("expected block, native or nil")))
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "map",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var kTyp, vTyp Ident
			if t, ok := typ.Expr.(*ast.MapType); ok {
				var err error
				kTyp, err = NewIdent(ctx, typ.File, t.Key)
				if err != nil {
					// TODO
					panic(err)
				}
				vTyp, err = NewIdent(ctx, typ.File, t.Value)
				if err != nil {
					// TODO
					panic(err)
				}
			} else {
				return false
			}

			allowedTyps := []string{"BlockType", "NativeType"}
			if kTyp.GoName == "string" {
				allowedTyps = append(allowedTyps, "DictType")
			}

			convAndInsert := func(inKeyVar, inValVar string, convKey bool) bool {
				if convKey {
					cb.Linef(`var mapK %v`, kTyp.GoName)
					ctx.MarkUsed(kTyp)
					if _, found := ConvRyeToGo(
						ctx,
						cb,
						kTyp,
						`mapK`,
						inKeyVar,
						argn,
						func(inner string) string {
							return makeRetConvErr("map key: " + inner)
						},
					); !found {
						return false
					}
				} else {
					cb.Linef(`mapK := %v`, inKeyVar)
				}
				cb.Linef(`var mapV %v`, vTyp.GoName)
				ctx.MarkUsed(vTyp)
				if _, found := ConvRyeToGo(
					ctx,
					cb,
					vTyp,
					`mapV`,
					inValVar,
					argn,
					func(inner string) string {
						return makeRetConvErr("map value: " + inner)
					},
				); !found {
					return false
				}
				cb.Linef(`%v[mapK] = mapV`, outVar)
				return true
			}

			cb.Linef(`switch v := %v.(type) {`, inVar)
			cb.Linef(`case env.Block:`)
			cb.Indent++
			cb.Linef(`if len(v.Series.S) %% 2 != 0 {`)
			cb.Indent++
			cb.Append(makeRetConvErr(fmt.Sprintf("expected block to have length of multiple of 2")))
			cb.Indent--
			cb.Linef(`}`)
			cb.Linef(`%v = make(%v, len(v.Series.S)/2)`, outVar, typ.GoName)
			ctx.MarkUsed(typ)
			cb.Linef(`for i := 0; i < len(v.Series.S); i += 2 {`)
			cb.Indent++
			if !convAndInsert(`v.Series.S[i+0]`, `v.Series.S[i+1]`, true) {
				return false
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Indent--
			cb.Linef(`case env.Dict:`)
			cb.Indent++
			cb.Linef(`%v = make(%v, len(v.Data))`, outVar, typ.GoName)
			ctx.MarkUsed(typ)
			cb.Linef(`for dictK, dictV := range v.Data {`)
			cb.Indent++
			if !convAndInsert(`dictK`, `dictV`, false) {
				return false
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Indent--
			convRyeToGoCodeCaseNative(ctx, cb, typ, outVar, `v`, argn, makeRetConvErr)
			convRyeToGoCodeCaseNil(cb, outVar, `v`, argn, makeRetConvErr)
			cb.Linef(`default:`)
			cb.Indent++
			if kTyp.GoName == "string" {
				cb.Append(makeRetConvErr(fmt.Sprintf("expected native, block, dict or nil")))
			} else {
				cb.Append(makeRetConvErr(fmt.Sprintf("expected native, block or nil")))
			}
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "func",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var fnParams []NamedIdent
			var fnResults []NamedIdent
			switch t := typ.Expr.(type) {
			case *ast.FuncType:
				var err error
				fnParams, _, err = ParamsToIdents(ctx, typ.File, t.Params)
				if err != nil {
					// TODO
					panic(err)
				}
				if t.Results != nil {
					fnResults, _, err = ParamsToIdents(ctx, typ.File, t.Results)
					if err != nil {
						// TODO
						panic(err)
					}
				}
			default:
				return false
			}

			return ConvRyeToGoCodeFunc(ctx, cb, outVar, inVar, argn, makeRetConvErr, false, fnParams, fnResults)
		},
	},
	{
		Name: "builtin",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			id, ok := typ.Expr.(*ast.Ident)
			if !ok {
				return false
			}

			if id.Name == "error" {
				cb.Linef(`switch v := %v.(type) {`, inVar)
				cb.Linef(`case env.String:`)
				cb.Indent++
				cb.Linef(`%v = errors.New(v.Value)`, outVar)
				ctx.UsedImports["errors"] = struct{}{}
				cb.Indent--
				cb.Linef(`case env.Error:`)
				cb.Indent++
				cb.Linef(`%v = errors.New(v.Print(*ps.Idx))`, outVar)
				ctx.UsedImports["errors"] = struct{}{}
				cb.Indent--
				convRyeToGoCodeCaseNil(cb, outVar, `v`, argn, makeRetConvErr)
				cb.Linef(`default:`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf("expected error, string or nil")))
				cb.Indent--
				cb.Linef(`}`)
			} else {
				var ryeObj string
				var ryeObjType string
				if id.Name == "int" || id.Name == "uint" ||
					id.Name == "uint8" || id.Name == "uint16" || id.Name == "uint32" || id.Name == "uint64" ||
					id.Name == "int8" || id.Name == "int16" || id.Name == "int32" || id.Name == "int64" ||
					id.Name == "bool" {
					ryeObj = "Integer"
					ryeObjType = "integer"
				} else if id.Name == "float32" || id.Name == "float64" {
					ryeObj = "Decimal"
					ryeObjType = "decimal"
				} else if id.Name == "string" {
					ryeObj = "String"
					ryeObjType = "string"
				} else {
					return false
				}

				cb.Linef(`if v, ok := %v.(env.%v); ok {`, inVar, ryeObj)
				cb.Indent++
				if id.Name == "bool" {
					cb.Linef(`%v = v.Value != 0`, outVar)
				} else {
					cb.Linef(`%v = %v(v.Value)`, outVar, id.Name)
				}
				cb.Indent--
				cb.Linef(`} else {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf("expected %v", ryeObjType)))
				cb.Indent--
				cb.Linef(`}`)
			}

			return true
		},
	},
	{
		Name: "typedef",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			underlying, ok := getUnderlyingType(ctx, typ)
			if !ok {
				return false
			}

			cb.Linef(`{`)
			cb.Indent++
			cb.Linef(`nat, natOk := %v.(env.Native)`, inVar)
			cb.Linef(`var natValOk bool`)
			if IdentIsInternal(ctx, typ) {
				cb.Linef(`var rOut, rIn reflect.Value`)
				cb.Linef(`if natOk {`)
				cb.Indent++
				cb.Linef(`// HACK: %v, natValOk = %v(u)`, outVar, typ.GoName)
				cb.Linef(`rOut = reflect.ValueOf(&%v).Elem()`, outVar)
				cb.Linef(`rIn = reflect.ValueOf(nat.Value)`)
				cb.Linef(`natValOk = rIn.CanConvert(rOut.Type())`)
				cb.Indent--
				cb.Linef(`}`)
			} else {
				cb.Linef(`var natVal %v`, typ.GoName)
				ctx.MarkUsed(typ)
				cb.Linef(`if natOk {`)
				cb.Indent++
				cb.Linef(`natVal, natValOk = nat.Value.(%v)`, typ.GoName)
				ctx.MarkUsed(typ)
				cb.Indent--
				cb.Linef(`}`)
			}
			cb.Linef(`if natValOk {`)
			cb.Indent++
			if IdentIsInternal(ctx, typ) {
				cb.Linef(`rOut.Set(rIn.Convert(rOut.Type()))`)
			} else {
				cb.Linef(`%v = natVal`, outVar)
			}
			cb.Indent--
			cb.Linef(`} else {`)
			cb.Indent++
			cb.Linef(`var u %v`, underlying.GoName)
			ctx.MarkUsed(underlying)
			if _, found := ConvRyeToGo(
				ctx,
				cb,
				underlying,
				`u`,
				inVar,
				argn,
				func(inner string) string {
					return makeRetConvErr(inner)
				},
			); !found {
				return false
			}
			if IdentIsInternal(ctx, typ) {
				cb.Linef(`// HACK: %v = %v(u)`, outVar, typ.GoName)
				cb.Linef(`rOut := reflect.ValueOf(&%v).Elem()`, outVar)
				cb.Linef(`rIn := reflect.ValueOf(u)`)
				cb.Linef(`rOut.Set(rIn.Convert(rOut.Type()))`)
				ctx.UsedImports["reflect"] = struct{}{}
			} else {
				cb.Linef(`%v = %v(u)`, outVar, typ.GoName)
				ctx.MarkUsed(typ)
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "native",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			isNillable := false
			switch typ.Expr.(type) {
			case *ast.StarExpr, *ast.ArrayType:
				isNillable = true
			}
			if _, exists := ctx.Data.Interfaces[typ.GoName]; exists {
				isNillable = true
			}

			cb.Linef(`switch v := %v.(type) {`, inVar)
			iface, isIface := ctx.Data.Interfaces[typ.GoName]
			if isIface && !iface.HasPrivateFields {
				ctx.Data.RequiredIfaces[iface.Name.GoName] = iface
				cb.Linef(`case env.RyeCtx:`)
				cb.Indent++
				cb.Linef(`var err error`)
				cb.Linef(`%v, err = ctxTo_%v(ps, v)`, outVar, strings.ReplaceAll(iface.Name.GoName, ".", "_"))
				cb.Linef(`if err != nil {`)
				cb.Indent++
				cb.Append(makeRetConvErr(`"+err.Error()+"`))
				cb.Indent--
				cb.Linef(`}`)
				cb.Indent--
			}
			cb.Linef(`case env.Native:`)
			cb.Indent++
			if IdentIsInternal(ctx, typ) {
				cb.Linef(`// HACK: %v, ok = v.Value.(%v)`, outVar, typ.GoName)
				cb.Linef(`rOut := reflect.ValueOf(&%v).Elem()`, outVar)
				cb.Linef(`rIn := reflect.ValueOf(v.Value)`)
				cb.Linef(`if rIn.CanConvert(rOut.Type()) {`)
				cb.Indent++
				cb.Linef(`rOut.Set(rIn.Convert(rOut.Type()))`)
				cb.Indent--
				cb.Linef(`} else {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf("expected native of type %v", typ.GoName)))
				cb.Indent--
				cb.Linef(`}`)
				ctx.UsedImports["reflect"] = struct{}{}
			} else {
				cb.Linef(`var ok bool`)
				cb.Linef(`%v, ok = v.Value.(%v)`, outVar, typ.GoName)
				ctx.MarkUsed(typ)
				cb.Linef(`if !ok {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf("expected native of type %v", typ.GoName)))
				cb.Indent--
				cb.Linef(`}`)
			}
			cb.Indent--
			if isNillable {
				cb.Linef(`case env.Integer:`)
				cb.Indent++
				cb.Linef(`if v.Value != 0 {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf("expected integer to be 0 or nil")))
				cb.Indent--
				cb.Linef(`}`)
				cb.Linef(`%v = nil`, outVar)
				cb.Indent--
			}
			cb.Linef(`default:`)
			cb.Indent++
			cb.Append(makeRetConvErr(fmt.Sprintf("expected native")))
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
}

var convListGoToRye = []Converter{
	{
		Name: "array",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var elTyp Ident
			switch t := typ.Expr.(type) {
			case *ast.ArrayType:
				var err error
				elTyp, err = NewIdent(ctx, typ.File, t.Elt)
				if err != nil {
					// TODO
					panic(err)
				}
			case *ast.Ellipsis:
				var err error
				elTyp, err = NewIdent(ctx, typ.File, t.Elt)
				if err != nil {
					// TODO
					panic(err)
				}
			default:
				return false
			}

			cb.Linef(`{`)
			cb.Indent++
			cb.Linef(`items := make([]env.Object, len(%v))`, inVar)
			cb.Linef(`for i, it := range %v {`, inVar)
			cb.Indent++
			if _, found := ConvGoToRye(
				ctx,
				cb,
				elTyp,
				`items[i]`,
				`it`,
				argn,
				nil,
			); !found {
				return false
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Linef(`%v = *env.NewBlock(*env.NewTSeries(items))`, outVar)
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "map",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var kTyp, vTyp Ident
			if t, ok := typ.Expr.(*ast.MapType); ok {
				var err error
				kTyp, err = NewIdent(ctx, typ.File, t.Key)
				if err != nil {
					// TODO
					panic(err)
				}
				vTyp, err = NewIdent(ctx, typ.File, t.Value)
				if err != nil {
					// TODO
					panic(err)
				}
			} else {
				return false
			}

			if kTyp.GoName != "string" {
				return false
			}

			cb.Linef(`{`)
			cb.Indent++
			cb.Linef(`data := make(map[string]any, len(%v))`, inVar)
			cb.Linef(`for mKey, mVal := range %v {`, inVar)
			cb.Indent++
			cb.Linef(`var dVal env.Object`)
			if _, found := ConvGoToRye(
				ctx,
				cb,
				vTyp,
				`dVal`,
				`mVal`,
				argn,
				nil,
			); !found {
				return false
			}
			cb.Linef(`data[mKey] = dVal`)
			cb.Indent--
			cb.Linef(`}`)
			cb.Linef(`%v = *env.NewDict(data)`, outVar)
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "builtin",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			id, ok := typ.Expr.(*ast.Ident)
			if !ok {
				return false
			}

			if id.Name == "error" {
				cb.Linef(`if %v != nil {`, inVar)
				cb.Indent++
				cb.Linef(`%v = *env.NewError(%v.Error())`, outVar, inVar)
				cb.Indent--
				cb.Linef(`}`)
			} else {
				var convFmt string
				if id.Name == "int" || id.Name == "uint" ||
					id.Name == "uint8" || id.Name == "uint16" || id.Name == "uint32" || id.Name == "uint64" ||
					id.Name == "int8" || id.Name == "int16" || id.Name == "int32" || id.Name == "int64" {
					convFmt = `*env.NewInteger(int64(%v))`
				} else if id.Name == "bool" {
					convFmt = `*env.NewInteger(boolToInt64(%v))`
				} else if id.Name == "float32" || id.Name == "float64" {
					convFmt = `*env.NewDecimal(float64(%v))`
				} else if id.Name == "string" {
					convFmt = `*env.NewString(%v)`
				} else {
					return false
				}

				cb.Linef(`%v = %v`, outVar, fmt.Sprintf(convFmt, inVar))
			}
			return true
		},
	},
	{
		Name: "native",
		TryConv: func(ctx *Context, cb *CodeBuilder, typ Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			isInterface := false
			if _, ok := ctx.Data.Interfaces[typ.GoName]; ok {
				cb.Linef(`{`)
				cb.Indent++
				cb.Linef(`typ := reflect.TypeOf(%v)`, inVar)
				cb.Linef(`var typPfx string`)
				cb.Linef(`if typ.Kind() == reflect.Pointer {`)
				cb.Indent++
				cb.Linef(`typPfx = "*"`)
				cb.Linef(`typ = typ.Elem()`)
				cb.Indent--
				cb.Linef(`}`)
				ctx.UsedImports["reflect"] = struct{}{}
				cb.Linef(`typRyeName, ok := ryeStructNameLookup[typ.PkgPath() + "." + typPfx + typ.Name()]`)
				isInterface = true
			}
			if underlying, ok := getUnderlyingType(ctx, typ); ok {
				if _, found := ConvGoToRye(
					ctx,
					cb,
					underlying,
					outVar,
					fmt.Sprintf(`%v(%v)`, underlying.GoName, inVar),
					argn,
					nil,
				); !found {
					return false
				}
			} else {
				if isInterface {
					cb.Linef(`if ok {`)
					cb.Indent++
					cb.Linef(`%v = *env.NewNative(ps.Idx, %v, typRyeName)`, outVar, inVar)
					cb.Indent--
					cb.Linef(`} else {`)
					cb.Indent++
					cb.Linef(`%v = *env.NewNative(ps.Idx, %v, "%v")`, outVar, inVar, typ.RyeName)
					cb.Indent--
					cb.Linef(`}`)
				} else {
					cb.Linef(`%v = *env.NewNative(ps.Idx, %v, "%v")`, outVar, inVar, typ.RyeName)
				}
			}
			if isInterface {
				cb.Indent--
				cb.Linef(`}`)
			}
			return true
		},
	},
}
