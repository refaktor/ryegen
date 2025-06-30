var typeLookup = map[string]map[string]string{}
func conv_map_int_int_toRye(ps *_env.ProgramState, s map[int]int) (_env.Native, error) {
	return *_env.NewNative(ps.Idx, &s, "go(*map[int]int)"), nil
}