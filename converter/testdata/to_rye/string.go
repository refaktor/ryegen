var pkgLookup = make(map[string]string, 0)
func conv_string_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x string) (_env.String, error) {
	return *_env.NewString(x), nil
}