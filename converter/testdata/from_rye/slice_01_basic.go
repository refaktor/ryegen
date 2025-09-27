var typeLookup = map[string]map[string]string{}
func conv_slice_int_fromRye(ps *_env.ProgramState, obj _env.Object) ([]int, error) {
	if blk, ok := obj.(_env.Block); ok {
		items := make([]int, len(blk.Series.S))
		for i, v := range blk.Series.S {
			var err error
			items[i], err = conv_int_fromRye(ps, v)
			if err != nil {
				return nil, err
			}
		}
		return items, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.([]int); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected block of type " + "int" + ", but got " + objectType(ps, obj))
}

func conv_int_fromRye(ps *_env.ProgramState, obj _env.Object) (int, error) {
	if x, ok := obj.(_env.Integer); ok {
		return int(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(int); ok {
			return v, nil
		}
	}
	return 0, _errors.New("expected int, but got " + objectType(ps, obj))
}