var typeLookup = map[string]map[string]string{}
func conv_float64_toRye(ps *_env.ProgramState, x float64) (_env.Decimal, error) {
	return *_env.NewDecimal(float64(x)), nil
}