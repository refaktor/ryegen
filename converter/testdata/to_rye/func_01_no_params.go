var pkgLookup = make(map[string]string, 0)
func conv_func_1926bfa0a15a6c3c_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, fn func()) (_env.VarBuiltin, error) {
	outfnErrable := func(ps *_env.ProgramState, ctx *_env.RyeCtx, args ..._env.Object) (_env.Object, error) {
		fn()
		if ps.ErrorFlag || ps.FailureFlag {
			if _, ok := ps.Res.(*_env.Error); !ok {
				ps.Res = _env.NewError("callback aborted: " + ps.Res.Inspect(*ps.Idx))
			}
			return ps.Res, nil
		}
		return *_env.NewVoid(), nil
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