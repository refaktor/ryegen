func conv_array_69_int_toRye(ps *_env.ProgramState, a [69]int) (_env.Block, error) {
	var items [69]_env.Object
	for i := range a {
		var err error
		items[i], err = conv_int_toRye(ps, a[i])
		if err != nil {
			return _env.Block{}, err
		}
	}
	return *_env.NewBlock(*_env.NewTSeries(items[:])), nil
}

func conv_int_toRye(ps *_env.ProgramState, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}