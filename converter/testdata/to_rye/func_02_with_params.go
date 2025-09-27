var typeLookup = map[string]map[string]string{}
func conv_func_233d01fb786b160a_toRye(ps *_env.ProgramState, fn func(a int, b int, c string, d []string)) (_env.VarBuiltin, error) {
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
		arg3, err := conv_slice_string_fromRye(ps, args[3])
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

func conv_slice_string_fromRye(ps *_env.ProgramState, obj _env.Object) ([]string, error) {
	if blk, ok := obj.(_env.Block); ok {
		items := make([]string, len(blk.Series.S))
		for i, v := range blk.Series.S {
			var err error
			items[i], err = conv_string_fromRye(ps, v)
			if err != nil {
				return nil, err
			}
		}
		return items, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.([]string); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected block of type " + "string" + ", but got " + objectType(ps, obj))
}

func conv_int_fromRye(ps *_env.ProgramState, obj _env.Object) (int, error) {
	if x, ok := obj.(_env.Integer); ok {
		return int(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(int); ok {
			return v, nil
		}
	}
	return 0, _errors.New("expected int, but got " + objectType(ps, obj))
}

func conv_string_fromRye(ps *_env.ProgramState, obj _env.Object) (string, error) {
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