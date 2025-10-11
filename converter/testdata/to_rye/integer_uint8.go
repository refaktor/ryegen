var typeLookup = map[string]map[string]string{}
func conv_uint8_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x uint8) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}