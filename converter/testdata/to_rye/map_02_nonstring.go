var pkgLookup = make(map[string]string, 0)
func conv_map_int_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, s map[int]int) (_env.Object, error) {
	return *_env.NewNative(ps.Idx, &s, "go(*map[int]int)"), nil
}