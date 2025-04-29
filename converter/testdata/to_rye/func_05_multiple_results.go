func conv_func_ac731ded5a42d0a5_toRye(ps *_env.ProgramState, fn func() (string, int, map[string]string)) (_env.VarBuiltin, error) {
	outfnErrable := func(ps *_env.ProgramState, args ..._env.Object) (_env.Object, error) {
		res0, res1, res2 := fn()
		outRes0, err := conv_string_toRye(ps, res0)
		if err != nil {
			return *_env.NewVoid(), err
		}
		outRes1, err := conv_int_toRye(ps, res1)
		if err != nil {
			return *_env.NewVoid(), err
		}
		outRes2, err := conv_map_string_string_toRye(ps, res2)
		if err != nil {
			return *_env.NewVoid(), err
		}
		return *_env.NewBlock(*_env.NewTSeries([]_env.Object{outRes0, outRes1, outRes2})), nil
	}

	return _env.VarBuiltin{
		Argsn: 0,
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