var typeLookup = map[string]map[string]string{}
func conv_any_fromRye(ps *_env.ProgramState, obj _env.Object) (any, error) {
	if nat, ok := obj.(_env.Native); ok {
		return nat.Value, nil
	}
	return nil, _errors.New("expected Native, but got " + objectType(ps, obj))
}