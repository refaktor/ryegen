func conv_map_int_int_toRye(ps *_env.ProgramState, m map[int]int) (_env.Dict, error) {
	return *_env.NewNative(ps.Idx, m, "go(map[int]int)"), nil
}