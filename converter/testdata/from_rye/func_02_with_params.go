var typeLookup = map[string]map[string]string{}
func conv_slice_int_toRye(ps *_env.ProgramState, a []int) (_env.Block, error) {
	items := make([]_env.Object, len(a))
	for i := range a {
		var err error
		items[i], err = conv_int_toRye(ps, a[i])
		if err != nil {
			return _env.Block{}, err
		}
	}
	return *_env.NewBlock(*_env.NewTSeries(items)), nil
}

func conv_int_toRye(ps *_env.ProgramState, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}

func conv_string_toRye(ps *_env.ProgramState, x string) (_env.String, error) {
	return *_env.NewString(x), nil
}

func conv_func_12c572ea71e5b4ba_fromRye(ps *_env.ProgramState, obj _env.Object) (func(a int, b int, c string, d []int), error) {
	if isNil(obj) {
		return nil, nil
	}
	if fn, ok := obj.(_env.Function); ok {
		if fn.Argsn != 4 {
			return nil, _errors.New("expected function with 4 args, but got " + objectType(ps, obj))
		}
		return func(inArg0 int, inArg1 int, inArg2 string, inArg3 []int) {
			arg0, err := conv_int_toRye(ps, inArg0)
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			arg1, err := conv_int_toRye(ps, inArg1)
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			arg2, err := conv_string_toRye(ps, inArg2)
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			arg3, err := conv_slice_int_toRye(ps, inArg3)
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			_evaldo.CallFunctionArgsN(fn, ps, ps.Ctx, arg0, arg1, arg2, arg3)
			if e, ok := ps.Res.(*_env.Error); ok {
				showFunctionError(ps, fn, _errors.New(e.Message))
				return
			}
		}, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(func(a int, b int, c string, d []int)); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected function or native of type go(" + "func(a int, b int, c string, d []int)" + "), but got " + objectType(ps, obj))
}