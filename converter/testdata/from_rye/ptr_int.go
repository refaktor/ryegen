var pkgLookup = make(map[string]string, 0)
func conv_ptr_int_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (*int, error) {
	if isNil(obj) {
		return nil, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(*int); ok {
			return v, nil
		}
	}
	if x, err := conv_int_fromRye(ps, ctx, obj); err == nil {
		return &x, nil
	}
	return nil, _errors.New("expected Native of type " + "*int" + ", or any element type, but got " + objectType(ps, obj))
}

func conv_int_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (int, error) {
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