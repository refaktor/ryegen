func conv_error_fromRye(ps *_env.ProgramState, obj _env.Object) (error, error) {
	if isNil(obj) {
		return nil, nil
	}
	if x, ok := obj.(_env.Error); ok {
		return _errors.New(x.Print(*ps.Idx)), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(error); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected error, but got " + objectType(ps, obj))
}