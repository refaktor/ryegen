var pkgLookup = make(map[string]string, 0)
func conv_array_69_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, a [69]int) (_env.Block, error) {
	var items [69]_env.Object
	for i := range a {
		var err error
		items[i], err = conv_int_toRye(ps, ctx, a[i])
		if err != nil {
			return _env.Block{}, err
		}
	}
	return *_env.NewBlock(*_env.NewTSeries(items[:])), nil
}

func conv_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}