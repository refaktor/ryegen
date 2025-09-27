var typeLookup = map[string]map[string]string{}
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

func conv_func_c2fb56a42cb0385a_fromRye(ps *_env.ProgramState, obj _env.Object) (func() (string, int, []string), error) {
	if isNil(obj) {
		return nil, nil
	}
	if fn, ok := obj.(_env.Function); ok {
		if fn.Argsn != 0 {
			return nil, _errors.New("expected function with 0 args, but got " + objectType(ps, obj))
		}
		return func() (_ string, _ int, _ []string) {
			_evaldo.CallFunctionArgsN(fn, ps, ps.Ctx)
			if e, ok := ps.Res.(*_env.Error); ok {
				showFunctionError(ps, fn, _errors.New(e.Message))
				return
			}
			blk, ok := ps.Res.(_env.Block)
			if !ok {
				showFunctionError(ps, fn, _errors.New("expected block with results, but got " +  objectType(ps, ps.Res)))
				return
			}
			if len(blk.Series.S) != 3 {
				showFunctionError(ps, fn, _fmt.Errorf("expected 3 results, but got %v", len(blk.Series.S)))
				return
			}
			res0, err := conv_string_fromRye(ps, blk.Series.S[0])
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			res1, err := conv_int_fromRye(ps, blk.Series.S[1])
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			res2, err := conv_slice_string_fromRye(ps, blk.Series.S[2])
			if err != nil {
				showFunctionError(ps, fn, err)
				return
			}
			return res0, res1, res2
		}, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(func() (string, int, []string)); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected function or native of type go(" + "func() (string, int, []string)" + "), but got " + objectType(ps, obj))
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