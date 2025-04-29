func conv_int_toRye(ps *_env.ProgramState, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}