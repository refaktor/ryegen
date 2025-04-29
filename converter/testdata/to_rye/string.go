func conv_string_toRye(ps *_env.ProgramState, x string) (_env.String, error) {
	return *_env.NewString(x), nil
}