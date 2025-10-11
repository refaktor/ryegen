var typeLookup = map[string]map[string]string{}
func conv_float64_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x float64) (_env.Decimal, error) {
	return *_env.NewDecimal(float64(x)), nil
}