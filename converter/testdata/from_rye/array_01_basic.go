func conv_array_69_int_fromRye(ps *_env.ProgramState, obj _env.Object) ([69]int, error) {
	if blk, ok := obj.(_env.Block); ok {
		if len(blk.Series.S) != 69 {
			return [69]int{}, _errors.New("expected block of type int to be of length 69, but got " + objectType(ps, obj))
		}
		var items [69]int
		for i, v := range blk.Series.S {
			var err error
			items[i], err = conv_int_fromRye(ps, v)
			if err != nil {
				return [69]int{}, err
			}
		}
		return items, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.([69]int); ok {
			return v, nil
		}
	}
	return [69]int{}, _errors.New("expected block of type int, but got " + objectType(ps, obj))
}