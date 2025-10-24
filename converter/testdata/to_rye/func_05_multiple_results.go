var pkgLookup = make(map[string]string, 0)
func conv_func_ac731ded5a42d0a5_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, fn func() (string, int, map[string]string)) (_env.VarBuiltin, error) {
	outfnErrable := func(ps *_env.ProgramState, ctx *_env.RyeCtx, args ..._env.Object) (_env.Object, error) {
		res0, res1, res2 := fn()
		outRes0, err := conv_string_toRye(ps, ctx, res0)
		if err != nil {
			return *_env.NewVoid(), err
		}
		outRes1, err := conv_int_toRye(ps, ctx, res1)
		if err != nil {
			return *_env.NewVoid(), err
		}
		outRes2, err := conv_map_string_string_toRye(ps, ctx, res2)
		if err != nil {
			return *_env.NewVoid(), err
		}
		return *_env.NewBlock(*_env.NewTSeries([]_env.Object{outRes0, outRes1, outRes2})), nil
	}

	return _env.VarBuiltin{
		Argsn: 0,
		Fn: func(ps *_env.ProgramState, args ..._env.Object) _env.Object {
			res, err := outfnErrable(ps, ps.Ctx, args...)
			if err != nil {
				ps.FailureFlag = true
				return _env.NewError(err.Error())
			}
			return res
		},
	}, nil
}

func conv_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}

func conv_map_string_string_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, m map[string]string) (_env.Dict, error) {
	data := make(map[string]any, len(m))
	for k, v := range m {
		v1, err := conv_string_toRye(ps, ctx, v)
		if err != nil {
			return _env.Dict{}, err
		}
		data[k] = v1
	}
	return *_env.NewDict(data), nil
}

func conv_string_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x string) (_env.String, error) {
	return *_env.NewString(x), nil
}