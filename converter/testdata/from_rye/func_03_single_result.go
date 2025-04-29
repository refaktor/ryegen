func conv_func_c4f955a1345caff5_fromRye(ps *_env.ProgramState, obj _env.Object) (func() string, error) {
	if fn, ok := obj.(_env.Function); ok {
		if fn.Argsn != 0 {
			return nil, _errors.New("expected function with 0 args, but got " + objectType(ps, obj))
		}
		return func() (_ string) {
			_evaldo.CallFunctionArgsN(fn, ps, ps.Ctx)
			if e, ok := ps.Res.(*_env.Error); ok {
				showFunctionError(*ps.Idx, fn, _errors.New(e.Message))
				return
			}
			res, err := conv_string_fromRye(ps, ps.Res)
			if err != nil {
				showFunctionError(*ps.Idx, fn, err)
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
	return nil, _errors.New("expected function or native of type go(func() string), but got " + objectType(ps, obj))
}