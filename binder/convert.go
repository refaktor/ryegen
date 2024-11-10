package binder

import (
	"errors"
	"fmt"
	"go/ast"
	"slices"
	"strconv"
	"strings"

	"github.com/refaktor/ryegen/binder/binderio"
	"github.com/refaktor/ryegen/ir"
)

type Converter struct {
	Name    string
	TryConv func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool
}

func ConvRyeToGo(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) (string, bool) {
	for _, conv := range ConvListRyeToGo {
		if conv.TryConv(deps, ctx, cb, typ, outVar, inVar, argn, makeRetConvErr) {
			return conv.Name, true
		}
	}
	return "", false
}

func ConvGoToRye(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) (string, bool) {
	for _, conv := range ConvListGoToRye {
		if conv.TryConv(deps, ctx, cb, typ, outVar, inVar, argn, makeRetConvErr) {
			return conv.Name, true
		}
	}
	return "", false
}

// Resolves the typedef chain. Won't resolve to an internal type.
func getUnderlyingType(ctx *Context, typ ir.Ident) (ir.Ident, bool) {
	retOk := false
	for {
		if underlying, ok := ctx.IR.Typedefs[typ.Name]; ok &&
			!ir.IdentIsInternal(ctx.ModNames, underlying) {
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

func convRyeToGoCodeCaseNil(deps *Dependencies, cb *binderio.CodeBuilder, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) {
	cb.Linef(`case env.Integer:`)
	cb.Indent++
	cb.Linef(`if %v.Value != 0 {`, inVar)
	cb.Indent++
	cb.Append(makeRetConvErr(fmt.Sprintf(`"expected integer to be 0 or nil, but got "+strconv.FormatInt(%v.Value, 10)`, inVar)))
	deps.Imports["strconv"] = struct{}{}
	cb.Indent--
	cb.Linef(`}`)
	cb.Linef(`%v = nil`, outVar)
	cb.Indent--
}

func ConvRyeToGoCodeFunc(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, outVar, inVar string, canBeNil bool, argn int, makeRetConvErr func(inner string) string, ctxAsArg0 bool, params, results []ir.NamedIdent) bool {
	var fnTyp string
	{
		var fnTypB strings.Builder
		fnTypB.WriteString("func(")
		nParamsWritten := 0
		if ctxAsArg0 {
			fnTypB.WriteString("ctx env.RyeCtx")
			nParamsWritten++
		}
		for i, param := range params {
			if nParamsWritten != 0 {
				fnTypB.WriteString(", ")
			}
			fnTypB.WriteString(fmt.Sprintf("farg%v %v", i, param.Type.Name))
			deps.MarkUsed(param.Type)
			nParamsWritten++
		}
		fnTypB.WriteString(")")
		if len(results) > 0 {
			fnTypB.WriteString(" (")
			for i, result := range results {
				if i != 0 {
					fnTypB.WriteString(", ")
				}
				fnTypB.WriteString(result.Type.Name)
				deps.MarkUsed(result.Type)
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
	cb.Append(makeRetConvErr(fmt.Sprintf(`"expected %v function arguments, but got "+strconv.Itoa(fn.Argsn)`, len(params))))
	deps.Imports["strconv"] = struct{}{}
	cb.Indent--
	cb.Linef(`}`)

	cb.Linef(`%v = %v {`, outVar, fnTyp)
	cb.Indent++
	var argVals strings.Builder
	for i := range params {
		if i != 0 {
			argVals.WriteString(", ")
		}
		argVals.WriteString(fmt.Sprintf("farg%vVal", i))
	}
	if len(params) > 0 {
		cb.Linef(`var %v env.Object`, argVals.String())
	}
	for i, param := range params {
		if _, found := ConvGoToRye(
			deps,
			ctx,
			cb,
			param.Type,
			fmt.Sprintf(`farg%vVal`, i),
			fmt.Sprintf(`farg%v`, i),
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
		deps.Imports["fmt"] = struct{}{}
		deps.Imports["errors"] = struct{}{}
		var cb binderio.CodeBuilder
		cb.Linef(`ps.FailureFlag = true`)
		cb.Linef(`fmt.Printf("\033[31mError: \033[1m%%v\033[m\n\033[31mFrom function \033[1m%%v { %%v }\033[m\n",`)
		cb.Indent++
		cb.Linef(`"((RYEGEN:FUNCNAME)): arg %v: callback result: "+%v,`, argn+1, inner)
		cb.Linef(`actualFn.Spec.Series.PositionAndSurroundingElements(*ps.Idx),`)
		cb.Linef(`actualFn.Body.Series.PositionAndSurroundingElements(*ps.Idx),`)
		cb.Indent--
		cb.Linef(`)`)
		cb.Linef(`%v`, retStmt)
		return cb.String()
	}
	ctxIdent := "ps.Ctx"
	if ctxAsArg0 {
		ctxIdent = "&ctx"
	}
	argValsComma := ""
	if len(params) > 0 {
		argValsComma = ", "
	}
	cb.Linef(`evaldo.CallFunctionArgsN(fn, ps, %v%v%v)`, ctxIdent, argValsComma, argVals.String())
	if len(results) == 1 {
		cb.Linef(`var res %v`, results[0].Type.Name)
		deps.MarkUsed(results[0].Type)
		if _, found := ConvRyeToGo(
			deps,
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
			cb.Linef(`var res%v %v`, i, res.Type.Name)
		}
		cb.Linef(`res, ok := ps.Res.(env.Block)`)
		cb.Linef(`if !ok {`)
		cb.Indent++
		cb.Append(makeFnResultRetConvErr(`"expected block for multiple return values, but got "+objectDebugString(ps.Idx, ps.Res)`))
		cb.Indent--
		cb.Linef(`}`)
		cb.Linef(`if len(res.Series.S) != %v {`, len(results))
		cb.Indent++
		cb.Append(makeFnResultRetConvErr(fmt.Sprintf(`"expected block with %v return values, but got "+strconv.Itoa(len(res.Series.S))+" return values"`, len(results))))
		deps.Imports["strconv"] = struct{}{}
		cb.Indent--
		cb.Linef(`}`)
		for i, res := range results {
			deps.MarkUsed(res.Type)
			if _, found := ConvRyeToGo(
				deps,
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
	if canBeNil {
		convRyeToGoCodeCaseNil(deps, cb, outVar, `fn`, argn, makeRetConvErr)
	}
	cb.Linef(`default:`)
	cb.Indent++
	var expectErrStr string
	if canBeNil {
		expectErrStr = `"expected function or nil, but got "+objectDebugString(ps.Idx, fn)`
	} else {
		expectErrStr = `"expected function, but got "+objectDebugString(ps.Idx, fn)`
	}
	cb.Append(makeRetConvErr(expectErrStr))
	cb.Indent--
	cb.Linef(`}`)
	return true
}

func ConvGoToRyeCodeFuncBody(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, inVar string, makeRetConvErr func(inner string) string, recv *ir.Ident, params, results []ir.NamedIdent) error {
	params = slices.Clone(params)
	if recv != nil {
		recvName, _ := ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, nil, &ast.Ident{Name: "__recv"})
		params = append([]ir.NamedIdent{{Name: recvName, Type: *recv}}, params...)
	}

	if len(params) > 5 {
		return errors.New("can only handle at most 5 parameters")
	}

	hasOpaqueParam := false
	derefParam := make([]bool, len(params))
	for i, param := range params {
		if ir.IdentIsInternal(ctx.ModNames, param.Type) {
			// Internal types cannot be imported, meaning
			// we have to do everything opaquely using reflect
			hasOpaqueParam = true
			cb.Linef(`var arg%vVal any`, i)
		} else {
			cb.Linef(`var arg%vVal %v`, i, param.Type.Name)
			deps.MarkUsed(param.Type)
		}
		if _, found := ConvRyeToGo(
			deps,
			ctx,
			cb,
			param.Type,
			fmt.Sprintf(`arg%vVal`, i),
			fmt.Sprintf(`arg%v`, i),
			i,
			makeMakeRetArgErr(i),
		); !found {
			return errors.New("unhandled type conversion (rye to go): " + param.Type.Name)
		}
	}

	var args strings.Builder
	{
		start := 0
		if recv != nil {
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
			argStr := fmt.Sprintf(`%varg%vVal%v`, deref, i, expand)
			if hasOpaqueParam {
				args.WriteString(fmt.Sprintf(`reflect.ValueOf(%v)`, argStr))
				deps.Imports["reflect"] = struct{}{}
			} else {
				args.WriteString(argStr)
			}
		}
	}

	resultsWithoutErr := results
	var errResult *ir.NamedIdent
	if len(results) > 0 && results[len(results)-1].Type.Name == "error" {
		resultsWithoutErr = results[:len(results)-1]
		errResult = &results[len(results)-1]
	}

	resultIdxName := func(i int) string {
		if errResult != nil && i == len(results)-1 {
			return "Err"
		}
		return strconv.Itoa(i)
	}

	recvStr := ""
	if recv != nil {
		if derefParam[0] {
			recvStr = `(*arg0Val).`
		} else {
			recvStr = `arg0Val.`
		}
	}
	if hasOpaqueParam {
		cb.Linef(`ress := reflect.ValueOf(%v%v).Call([]reflect.Value{%v})`, recvStr, inVar, args.String())
		deps.Imports["reflect"] = struct{}{}
		cb.Linef(`if len(ress) != %v {`, len(results))
		cb.Indent++
		cb.Linef(`panic("expected %v to have %v return values")`, inVar, len(results))
		cb.Indent--
		cb.Linef(`}`)
		for i, result := range results {
			if ir.IdentIsInternal(ctx.ModNames, result.Type) {
				cb.Linef(`var res%v any`, resultIdxName(i))
				cb.Linef(`if !ress[%v].IsNil() {`, i)
				cb.Indent++
				cb.Linef(`res%v = ress[%v].Interface()`, resultIdxName(i), i)
				cb.Indent--
				cb.Linef(`}`)
			} else {
				cb.Linef(`var res%v %v`, resultIdxName(i), result.Type.Name)
				deps.MarkUsed(result.Type)
				cb.Linef(`if !ress[%v].IsNil() {`, i)
				cb.Indent++
				cb.Linef(`res%v = ress[%v].Interface().(%v)`, resultIdxName(i), i, result.Type.Name)
				deps.MarkUsed(result.Type)
				cb.Indent--
				cb.Linef(`}`)
			}
		}
	} else {
		var assign strings.Builder
		{
			for i := range results {
				if i != 0 {
					assign.WriteString(`, `)
				}
				assign.WriteString(fmt.Sprintf(`res%v`, resultIdxName(i)))
			}
			if len(results) > 0 {
				assign.WriteString(` := `)
			}
		}
		cb.Linef(`%v%v%v(%v)`, assign.String(), recvStr, inVar, args.String())
	}

	for i, result := range results {
		if ir.IdentIsInternal(ctx.ModNames, result.Type) {
			cb.Linef(
				`res%vObj := ifaceToNative(ps.Idx, res%v, "%v")`,
				resultIdxName(i), resultIdxName(i),
				result.Type.RyeName(),
			)
		} else {
			cb.Linef(`var res%vObj env.Object`, resultIdxName(i))
			if _, found := ConvGoToRye(
				deps,
				ctx,
				cb,
				result.Type,
				fmt.Sprintf(`res%vObj`, resultIdxName(i)),
				fmt.Sprintf(`res%v`, resultIdxName(i)),
				-1,
				nil,
			); !found {
				return errors.New("unhandled type conversion (go to rye): " + result.Type.Name)
			}
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
		if recv == nil {
			cb.Linef(`return nil`)
		} else {
			cb.Linef(`return arg0`)
		}
	}

	return nil
}

var convListRyeToGo = []Converter{
	{
		Name: "array",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var elTyp ir.Ident
			switch t := typ.Expr.(type) {
			case *ast.ArrayType:
				var err error
				elTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Elt)
				if err != nil {
					// TODO
					panic(err)
				}
			case *ast.Ellipsis:
				var err error
				elTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Elt)
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
			cb.Linef(`%v = make(%v, len(v.Series.S))`, outVar, typ.Name)
			deps.MarkUsed(typ)
			cb.Linef(`for i, it := range v.Series.S {`)
			cb.Indent++
			if _, found := ConvRyeToGo(
				deps,
				ctx,
				cb,
				elTyp,
				fmt.Sprintf(`%v[i]`, outVar),
				`it`,
				argn,
				func(inner string) string {
					return makeRetConvErr(`"block item: "+` + inner)
				},
			); !found {
				return false
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Indent--
			convRyeToGoCodeCaseNil(deps, cb, outVar, `v`, argn, makeRetConvErr)
			cb.Linef(`default:`)
			cb.Indent++
			cb.Append(makeRetConvErr(`"expected block or nil, but got "+objectDebugString(ps.Idx, v)`))
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "map",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var kTyp, vTyp ir.Ident
			if t, ok := typ.Expr.(*ast.MapType); ok {
				var err error
				kTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Key)
				if err != nil {
					// TODO
					panic(err)
				}
				vTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Value)
				if err != nil {
					// TODO
					panic(err)
				}
			} else {
				return false
			}

			convAndInsert := func(inKeyVar, inValVar string, convKey bool) bool {
				if convKey {
					cb.Linef(`var mapK %v`, kTyp.Name)
					deps.MarkUsed(kTyp)
					if _, found := ConvRyeToGo(
						deps,
						ctx,
						cb,
						kTyp,
						`mapK`,
						inKeyVar,
						argn,
						func(inner string) string {
							return makeRetConvErr(`"map key: "+` + inner)
						},
					); !found {
						return false
					}
				} else {
					cb.Linef(`mapK := %v`, inKeyVar)
				}
				cb.Linef(`var mapV %v`, vTyp.Name)
				deps.MarkUsed(vTyp)
				if _, found := ConvRyeToGo(
					deps,
					ctx,
					cb,
					vTyp,
					`mapV`,
					inValVar,
					argn,
					func(inner string) string {
						return makeRetConvErr(`"map value: "+` + inner)
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
			cb.Append(makeRetConvErr(`"expected block to have length of multiple of 2, but got block with length "+strconv.Itoa(len(v.Series.S))`))
			deps.Imports["strconv"] = struct{}{}
			cb.Indent--
			cb.Linef(`}`)
			cb.Linef(`%v = make(%v, len(v.Series.S)/2)`, outVar, typ.Name)
			deps.MarkUsed(typ)
			cb.Linef(`for i := 0; i < len(v.Series.S); i += 2 {`)
			cb.Indent++
			if !convAndInsert(`v.Series.S[i+0]`, `v.Series.S[i+1]`, true) {
				return false
			}
			cb.Indent--
			cb.Linef(`}`)
			cb.Indent--
			if kTyp.Name == "string" {
				cb.Linef(`case env.Dict:`)
				cb.Indent++
				cb.Linef(`%v = make(%v, len(v.Data))`, outVar, typ.Name)
				deps.MarkUsed(typ)
				cb.Linef(`for dictK, dictV := range v.Data {`)
				cb.Indent++
				if !convAndInsert(`dictK`, `dictV`, false) {
					return false
				}
				cb.Indent--
				cb.Linef(`}`)
				cb.Indent--
			}
			convRyeToGoCodeCaseNil(deps, cb, outVar, `v`, argn, makeRetConvErr)
			cb.Linef(`default:`)
			cb.Indent++
			if kTyp.Name == "string" {
				cb.Append(makeRetConvErr(`"expected block, dict or nil, but got "+objectDebugString(ps.Idx, v)`))
			} else {
				cb.Append(makeRetConvErr(`"expected block or nil, but got "+objectDebugString(ps.Idx, v)`))
			}
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
	{
		Name: "func",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var fnParams []ir.NamedIdent
			var fnResults []ir.NamedIdent
			switch t := typ.Expr.(type) {
			case *ast.FuncType:
				var err error
				fnParams, _, err = ir.ParamsToIdents(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Params)
				if err != nil {
					// TODO
					panic(err)
				}
				if t.Results != nil {
					fnResults, _, err = ir.ParamsToIdents(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Results)
					if err != nil {
						// TODO
						panic(err)
					}
				}
			default:
				return false
			}

			return ConvRyeToGoCodeFunc(deps, ctx, cb, outVar, inVar, true, argn, makeRetConvErr, false, fnParams, fnResults)
		},
	},
	{
		Name: "builtin",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			id, ok := typ.Expr.(*ast.Ident)
			if !ok {
				return false
			}

			if id.Name == "error" {
				cb.Linef(`switch v := %v.(type) {`, inVar)
				cb.Linef(`case env.String:`)
				cb.Indent++
				cb.Linef(`%v = errors.New(v.Value)`, outVar)
				deps.Imports["errors"] = struct{}{}
				cb.Indent--
				cb.Linef(`case env.Error:`)
				cb.Indent++
				cb.Linef(`%v = errors.New(v.Print(*ps.Idx))`, outVar)
				deps.Imports["errors"] = struct{}{}
				cb.Indent--
				convRyeToGoCodeCaseNil(deps, cb, outVar, `v`, argn, makeRetConvErr)
				cb.Linef(`default:`)
				cb.Indent++
				cb.Append(makeRetConvErr(`"expected error, string or nil, but got "+objectDebugString(ps.Idx, v)`))
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

				cb.Linef(`if vc, ok := %v.(env.%v); ok {`, inVar, ryeObj)
				cb.Indent++
				if id.Name == "bool" {
					cb.Linef(`%v = vc.Value != 0`, outVar)
				} else {
					cb.Linef(`%v = %v(vc.Value)`, outVar, id.Name)
				}
				cb.Indent--
				cb.Linef(`} else {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf(`"expected %v, but got "+objectDebugString(ps.Idx, %v)`, ryeObjType, inVar)))
				cb.Indent--
				cb.Linef(`}`)
			}

			return true
		},
	},
	{
		Name: "typedef",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			underlying, ok := getUnderlyingType(ctx, typ)
			if !ok {
				return false
			}

			cb.Linef(`{`)
			cb.Indent++
			cb.Linef(`nat, natOk := %v.(env.Native)`, inVar)
			cb.Linef(`var natValOk bool`)
			if ir.IdentIsInternal(ctx.ModNames, typ) {
				cb.Linef(`var rOut, rIn reflect.Value`)
				cb.Linef(`if natOk {`)
				cb.Indent++
				cb.Linef(`// HACK: %v, natValOk = %v(u)`, outVar, typ.Name)
				cb.Linef(`rOut = reflect.ValueOf(&%v).Elem()`, outVar)
				cb.Linef(`rIn = reflect.ValueOf(nat.Value)`)
				cb.Linef(`natValOk = rIn.CanConvert(rOut.Type())`)
				cb.Indent--
				cb.Linef(`}`)
			} else {
				cb.Linef(`var natVal %v`, typ.Name)
				deps.MarkUsed(typ)
				cb.Linef(`if natOk {`)
				cb.Indent++
				cb.Linef(`natVal, natValOk = nat.Value.(%v)`, typ.Name)
				deps.MarkUsed(typ)
				cb.Indent--
				cb.Linef(`}`)
			}
			cb.Linef(`if natValOk {`)
			cb.Indent++
			if ir.IdentIsInternal(ctx.ModNames, typ) {
				cb.Linef(`rOut.Set(rIn.Convert(rOut.Type()))`)
			} else {
				cb.Linef(`%v = natVal`, outVar)
			}
			cb.Indent--
			cb.Linef(`} else {`)
			cb.Indent++
			cb.Linef(`var u %v`, underlying.Name)
			deps.MarkUsed(underlying)
			if _, found := ConvRyeToGo(
				deps,
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
			if ir.IdentIsInternal(ctx.ModNames, typ) {
				cb.Linef(`// HACK: %v = %v(u)`, outVar, typ.Name)
				cb.Linef(`rOut := reflect.ValueOf(&%v).Elem()`, outVar)
				cb.Linef(`rIn := reflect.ValueOf(u)`)
				cb.Linef(`rOut.Set(rIn.Convert(rOut.Type()))`)
				deps.Imports["reflect"] = struct{}{}
			} else {
				cb.Linef(`%v = %v(u)`, outVar, typ.Name)
				deps.MarkUsed(typ)
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
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			isNillable := false
			switch typ.Expr.(type) {
			case *ast.StarExpr, *ast.ArrayType:
				isNillable = true
			}
			if _, exists := ctx.IR.Interfaces[typ.Name]; exists {
				isNillable = true
			}

			cb.Linef(`switch v := %v.(type) {`, inVar)
			iface, isIface := ctx.IR.Interfaces[typ.Name]
			if isIface &&
				!iface.HasPrivateFields &&
				!ir.IdentIsInternal(ctx.ModNames, iface.Name) {
				deps.GenericInterfaceImpls[iface.Name.Name] = iface
				cb.Linef(`case env.RyeCtx:`)
				cb.Indent++
				cb.Linef(`var err error`)
				cb.Linef(`%v, err = ctxTo_%v(ps, v)`, outVar, strings.ReplaceAll(iface.Name.Name, ".", "_"))
				cb.Linef(`if err != nil {`)
				cb.Indent++
				cb.Append(makeRetConvErr(`err.Error()`))
				cb.Indent--
				cb.Linef(`}`)
				cb.Indent--
			}
			cb.Linef(`case env.Native:`)
			cb.Indent++
			if ir.IdentIsInternal(ctx.ModNames, typ) {
				cb.Linef(`// HACK: %v, ok = v.Value.(%v)`, outVar, typ.Name)
				cb.Linef(`rOut := reflect.ValueOf(&%v).Elem()`, outVar)
				cb.Linef(`rIn := reflect.ValueOf(v.Value)`)
				cb.Linef(`if rIn.CanConvert(rOut.Type()) {`)
				cb.Indent++
				cb.Linef(`rOut.Set(rIn.Convert(rOut.Type()))`)
				cb.Indent--
				cb.Linef(`} else {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf(`"expected native of type %v, but got "+objectDebugString(ps.Idx, v)`, typ.Name)))
				cb.Indent--
				cb.Linef(`}`)
				deps.Imports["reflect"] = struct{}{}
			} else {
				deref := ""
				ty := typ
				if _, ok := ctx.IR.Structs[typ.Name]; ok {
					var err error
					ty, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, ty.File, &ast.StarExpr{X: ty.Expr})
					if err != nil {
						panic(err)
					}
					deref = "*"
				}
				cb.Linef(`if vc, ok := v.Value.(%v); ok {`, ty.Name)
				deps.MarkUsed(ty)
				cb.Indent++
				cb.Linef(`%v = %vvc`, outVar, deref)
				cb.Indent--
				cb.Linef(`} else {`)
				cb.Indent++
				cb.Append(makeRetConvErr(fmt.Sprintf(`"expected native of type %v, but got "+objectDebugString(ps.Idx, v)`, ty.Name)))
				cb.Indent--
				cb.Linef(`}`)
			}
			cb.Indent--
			if isNillable {
				cb.Linef(`case env.Integer:`)
				cb.Indent++
				cb.Linef(`if v.Value != 0 {`)
				cb.Indent++
				cb.Append(makeRetConvErr(`"expected integer to be 0 or nil, but got "+strconv.FormatInt(v.Value, 10)`))
				deps.Imports["strconv"] = struct{}{}
				cb.Indent--
				cb.Linef(`}`)
				cb.Linef(`%v = nil`, outVar)
				cb.Indent--
			}
			cb.Linef(`default:`)
			cb.Indent++
			cb.Append(makeRetConvErr(fmt.Sprintf(`"expected native, but got "+objectDebugString(ps.Idx, v)`)))
			cb.Indent--
			cb.Linef(`}`)

			return true
		},
	},
}

var convListGoToRye = []Converter{
	{
		Name: "array",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var elTyp ir.Ident
			switch t := typ.Expr.(type) {
			case *ast.ArrayType:
				var err error
				elTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Elt)
				if err != nil {
					// TODO
					panic(err)
				}
			case *ast.Ellipsis:
				var err error
				elTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Elt)
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
				deps,
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
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var kTyp, vTyp ir.Ident
			if t, ok := typ.Expr.(*ast.MapType); ok {
				var err error
				kTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Key)
				if err != nil {
					// TODO
					panic(err)
				}
				vTyp, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Value)
				if err != nil {
					// TODO
					panic(err)
				}
			} else {
				return false
			}

			if kTyp.Name != "string" {
				return false
			}

			cb.Linef(`{`)
			cb.Indent++
			cb.Linef(`data := make(map[string]any, len(%v))`, inVar)
			cb.Linef(`for mKey, mVal := range %v {`, inVar)
			cb.Indent++
			cb.Linef(`var dVal env.Object`)
			if _, found := ConvGoToRye(
				deps,
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
		Name: "func",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			var fnParams []ir.NamedIdent
			var fnResults []ir.NamedIdent
			switch t := typ.Expr.(type) {
			case *ast.FuncType:
				var err error
				fnParams, _, err = ir.ParamsToIdents(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Params)
				if err != nil {
					// TODO
					panic(err)
				}
				if t.Results != nil {
					fnResults, _, err = ir.ParamsToIdents(ctx.IR.ConstValues, ctx.ModNames, typ.File, t.Results)
					if err != nil {
						// TODO
						panic(err)
					}
				}
			default:
				return false
			}

			cb.Linef(`%v = *env.NewBuiltin(func(ps *env.ProgramState, arg0, arg1, arg2, arg3, arg4 env.Object) env.Object {`, outVar)
			cb.Indent++
			if err := ConvGoToRyeCodeFuncBody(
				deps,
				ctx,
				cb,
				inVar,
				nil,
				nil,
				fnParams,
				fnResults,
			); err != nil {
				fmt.Println(err)
				return false
			}
			cb.Indent--
			cb.Linef(`}, %v, false, false, "Returned func")`, len(fnParams))

			return true
		},
	},
	{
		Name: "builtin",
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			id, ok := typ.Expr.(*ast.Ident)
			if !ok {
				return false
			}

			if id.Name == "error" {
				cb.Linef(`if %v != nil {`, inVar)
				cb.Indent++
				cb.Linef(`%v = env.NewError(%v.Error())`, outVar, inVar)
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
		TryConv: func(deps *Dependencies, ctx *Context, cb *binderio.CodeBuilder, typ ir.Ident, outVar, inVar string, argn int, makeRetConvErr func(inner string) string) bool {
			if underlying, ok := getUnderlyingType(ctx, typ); ok {
				var in string
				if _, isFunc := underlying.Expr.(*ast.FuncType); isFunc {
					in = fmt.Sprintf(`(%v)(%v)`, underlying.Name, inVar)
				} else {
					in = fmt.Sprintf(`%v(%v)`, underlying.Name, inVar)
				}
				if _, found := ConvGoToRye(
					deps,
					ctx,
					cb,
					underlying,
					outVar,
					in,
					argn,
					nil,
				); !found {
					return false
				}
			} else {
				if _, ok := ctx.IR.Interfaces[typ.Name]; ok {
					cb.Linef(`%v = ifaceToNative(ps.Idx, %v, "%v")`, outVar, inVar, typ.RyeName())
				} else {
					addr := ""
					ty := typ
					if _, ok := ctx.IR.Structs[ty.Name]; ok {
						var err error
						ty, err = ir.NewIdent(ctx.IR.ConstValues, ctx.ModNames, ty.File, &ast.StarExpr{X: ty.Expr})
						if err != nil {
							panic(err)
						}
						addr = "&"
					}
					cb.Linef(`%v = *env.NewNative(ps.Idx, %v%v, "%v")`, outVar, addr, inVar, ty.RyeName())
				}
			}
			return true
		},
	},
}
