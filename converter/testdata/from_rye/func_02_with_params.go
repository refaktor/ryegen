func conv_func_16892ac2e1c990f5_fromRye(ps *_env.ProgramState, obj _env.Object) (func(a int, b int, c string, d map[int]int), error) {
	if fn, ok := obj.(_env.Function); ok {
		if fn.Argsn != 4 {
			return nil, _errors.New("expected function with 4 args, but got " + objectType(ps, obj))
		}
		return func(inArg0 int, inArg1 int, inArg2 string, inArg3 map[int]int) {
			arg0, err := conv_int_toRye(ps, inArg0)
			if err != nil {
				showFunctionError(*ps.Idx, fn, err)
				return
			}
			arg1, err := conv_int_toRye(ps, inArg1)
			if err != nil {
				showFunctionError(*ps.Idx, fn, err)
				return
			}
			arg2, err := conv_string_toRye(ps, inArg2)
			if err != nil {
				showFunctionError(*ps.Idx, fn, err)
				return
			}
			arg3, err := conv_map_int_int_toRye(ps, inArg3)
			if err != nil {
				showFunctionError(*ps.Idx, fn, err)
				return
			}
			_evaldo.CallFunctionArgsN(fn, ps, ps.Ctx, arg0, arg1, arg2, arg3)
			if e, ok := ps.Res.(*_env.Error); ok {
				showFunctionError(*ps.Idx, fn, _errors.New(e.Message))
				return
			}
		}, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(func(a int, b int, c string, d map[int]int)); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected function or native of type go(func(a int, b int, c string, d map[int]int)), but got " + objectType(ps, obj))
}