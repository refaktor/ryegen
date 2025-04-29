func conv_func_16892ac2e1c990f5_toRye(ps *_env.ProgramState, fn func(a int, b int, c string, d map[int]int)) (_env.VarBuiltin, error) {
	outfnErrable := func(ps *_env.ProgramState, args ..._env.Object) (_env.Object, error) {
		arg0, err := conv_int_fromRye(ps, args[0])
		if err != nil {
			return *_env.NewVoid(), err
		}
		arg1, err := conv_int_fromRye(ps, args[1])
		if err != nil {
			return *_env.NewVoid(), err
		}
		arg2, err := conv_string_fromRye(ps, args[2])
		if err != nil {
			return *_env.NewVoid(), err
		}
		arg3, err := conv_map_int_int_fromRye(ps, args[3])
		if err != nil {
			return *_env.NewVoid(), err
		}
		fn(arg0, arg1, arg2, arg3)
		return *_env.NewVoid(), nil
	}

	return _env.VarBuiltin{
		Argsn: 4,
		Fn: func(ps *_env.ProgramState, args ..._env.Object) _env.Object {
			res, err := outfnErrable(ps, args...)
			if err != nil {
				ps.FailureFlag = true
				return _env.NewError(err.Error())
			}
			return res
		},
	}, nil
}