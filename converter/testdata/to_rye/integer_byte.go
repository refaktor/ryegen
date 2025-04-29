func conv_byte_toRye(ps *_env.ProgramState, x byte) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}