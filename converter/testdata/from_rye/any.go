var pkgLookup = make(map[string]string, 0)
func conv_any_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (any, error) {
	switch v := obj.(type) {
	case _env.Boolean:
		return v.Value, nil
	case _env.Complex:
		return v.Value, nil
	case _env.Date:
		return v.Value, nil
	case _env.Decimal:
		return v.Value, nil
	case _env.Email:
		return v.Address, nil
	case _env.Error:
		return _errors.New(v.Print(*ps.Idx)), nil
	case _env.Integer:
		return v.Value, nil
	case _env.Native:
		return v.Value, nil
	case _env.String:
		return v.Value, nil
	case _env.Time:
		return v.Value, nil
	case _env.Void:
		return nil, nil
	}

	return nil, _errors.New("expected primitive or Native, but got " + objectType(ps, obj))
}