var pkgLookup = make(map[string]string, 0)
func conv_byte_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x byte) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}