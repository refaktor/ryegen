func conv_func_e2cfa6537a62ff24_toRye(ps *_env.ProgramState, fn func() (string, error)) (_env.VarBuiltin, error) {
	outfnErrable := func(ps *_env.ProgramState, args ..._env.Object) (_env.Object, error) {
		res0, res1 := fn()
		if res1 != nil {
			return *_env.NewVoid(), res1
		}
		outRes0, err := conv_string_toRye(ps, res0)
		if err != nil {
			return *_env.NewVoid(), err
		}
		return outRes0, nil
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

func conv_string_toRye(ps *_env.ProgramState, x string) (_env.String, error) {
	return *_env.NewString(x), nil
}