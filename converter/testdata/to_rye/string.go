var typeLookup = map[string]map[string]string{}
func conv_string_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x string) (_env.String, error) {
	return *_env.NewString(x), nil
}