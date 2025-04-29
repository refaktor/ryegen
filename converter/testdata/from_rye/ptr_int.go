func conv_ptr_int_fromRye(ps *_env.ProgramState, obj _env.Object) (*int, error) {
	if isNil(obj) {
		return nil, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(*int); ok {
			return v, nil
		}
	}
	if x, err := conv_int_fromRye(ps, obj); err == nil {
		return &x, nil
	}
	return nil, _errors.New("expected Native of type *int, or any element type, but got " + objectType(ps, obj))
}