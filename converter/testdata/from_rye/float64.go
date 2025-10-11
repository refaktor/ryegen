var typeLookup = map[string]map[string]string{}
func conv_float64_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (float64, error) {
	if x, ok := obj.(_env.Decimal); ok {
		return float64(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(float64); ok {
			return v, nil
		}
	}
	return 0.0, _errors.New("expected float64, but got " + objectType(ps, obj))
}