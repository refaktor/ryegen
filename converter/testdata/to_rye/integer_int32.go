var typeLookup = map[string]map[string]string{}
func conv_int32_toRye(ps *_env.ProgramState, x int32) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}