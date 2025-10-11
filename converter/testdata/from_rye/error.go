var typeLookup = map[string]map[string]string{}
func init() {
	typeLookup[""] = map[string]string{}
	typeLookup[""]["error"] = "error"
}

// error
type interface_9f7452dd75d54d31 struct {
	ps *_env.ProgramState
	ctx *_env.RyeCtx
	fn_Error func() string
}
func (i *interface_9f7452dd75d54d31) Error() (_ string) {
	if i.fn_Error == nil {
		return
	}
	return i.fn_Error()
}
func conv_error_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (error, error) {
	if isNil(obj) {
		return nil, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(error); ok {
			return v, nil
		}
	}
	c, ok := obj.(_env.RyeCtx)
	if !ok {
		return nil, _errors.New("expected native interface or context with methods " + "Error" + ", but got " + objectType(ps, obj))
	}
	i := &interface_9f7452dd75d54d31{
		ps: ps,
		ctx: ctx,
	}
	var idx int
	idx, ok = ps.Idx.GetIndex("Error")
	if ok {
		var m _env.Object
		m, ok = c.Get(idx)
		if ok {
			fn, err := conv_func_c4f955a1345caff5_fromRye(ps, &c, m)
			if err != nil {
				return nil, err
			}
			i.fn_Error = fn
		}
	}
	if !ok {
		return nil, _errors.New("expected context with method Error() (string)")
	}
	return i, nil
}

func conv_func_c4f955a1345caff5_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (func() string, error) {
	if isNil(obj) {
		return nil, nil
	}
	if fn, ok := obj.(_env.Function); ok {
		if fn.Argsn != 0 {
			return nil, _errors.New("expected function with 0 args, but got " + objectType(ps, obj))
		}
		return func() (_ string) {
			_evaldo.CallFunctionArgsN(fn, ps, ctx)
			if e, ok := ps.Res.(*_env.Error); ok {
				showFunctionError(ps, fn, _errors.New(e.Message))
				return
			}
			res, err := conv_string_fromRye(ps, ctx, ps.Res)
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			return res
		}, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(func() string); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected function or native of type go(" + "func() string" + "), but got " + objectType(ps, obj))
}

func conv_string_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (string, error) {
	if x, ok := obj.(_env.String); ok {
		return x.Value, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(string); ok {
			return v, nil
		}
	}
	return "", _errors.New("expected string, but got " + objectType(ps, obj))
}