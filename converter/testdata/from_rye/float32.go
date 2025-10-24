var pkgLookup = make(map[string]string, 0)
func conv_float32_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (float32, error) {
	if x, ok := obj.(_env.Decimal); ok {
		return float32(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(float32); ok {
			return v, nil
		}
	}
	return 0.0, _errors.New("expected float32, but got " + objectType(ps, obj))
}