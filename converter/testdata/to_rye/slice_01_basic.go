func conv_slice_int_toRye(ps *_env.ProgramState, a []int) (_env.Block, error) {
	items := make([]_env.Object, len(a))
	for i := range a {
		var err error
		items[i], err = conv_int_toRye(ps, a[i])
		if err != nil {
			return _env.Block{}, err
		}
	}
	return *_env.NewBlock(*_env.NewTSeries(items)), nil
}