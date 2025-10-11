var typeLookup = map[string]map[string]string{}
func conv_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}

func conv_map_string_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, m map[string]int) (_env.Dict, error) {
	data := make(map[string]any, len(m))
	for k, v := range m {
		v1, err := conv_int_toRye(ps, ctx, v)
		if err != nil {
			return _env.Dict{}, err
		}
		data[k] = v1
	}
	return *_env.NewDict(data), nil
}