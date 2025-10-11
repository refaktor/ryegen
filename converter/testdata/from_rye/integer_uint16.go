var typeLookup = map[string]map[string]string{}
func conv_uint16_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (uint16, error) {
	if x, ok := obj.(_env.Integer); ok {
		return uint16(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(uint16); ok {
			return v, nil
		}
	}
	return 0, _errors.New("expected uint16, but got " + objectType(ps, obj))
}