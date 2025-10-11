var typeLookup = map[string]map[string]string{}
func conv_uint64_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (uint64, error) {
	if x, ok := obj.(_env.Integer); ok {
		return uint64(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(uint64); ok {
			return v, nil
		}
	}
	return 0, _errors.New("expected uint64, but got " + objectType(ps, obj))
}