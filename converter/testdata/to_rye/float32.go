var pkgLookup = make(map[string]string, 0)
func conv_float32_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x float32) (_env.Decimal, error) {
	return *_env.NewDecimal(float64(x)), nil
}