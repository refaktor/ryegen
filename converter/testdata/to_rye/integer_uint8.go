var pkgLookup = make(map[string]string, 0)
func conv_uint8_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x uint8) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}