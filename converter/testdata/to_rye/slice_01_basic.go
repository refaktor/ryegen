var typeLookup = map[string]map[string]string{}
func conv_slice_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, a []int) (_env.Block, error) {
	items := make([]_env.Object, len(a))
	for i := range a {
		var err error
		items[i], err = conv_int_toRye(ps, ctx, a[i])
		if err != nil {
			return _env.Block{}, err
		}
	}
	return *_env.NewBlock(*_env.NewTSeries(items)), nil
}

func conv_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}