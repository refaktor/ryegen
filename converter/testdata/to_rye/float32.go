func conv_float32_toRye(ps *_env.ProgramState, x float32) (_env.Decimal, error) {
	return *_env.NewDecimal(float64(x)), nil
}