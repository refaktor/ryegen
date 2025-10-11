var pkgLookup = make(map[string]string, 0)
func conv_int32_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int32) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}