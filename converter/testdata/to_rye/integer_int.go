var typeLookup = map[string]map[string]string{}
func conv_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}