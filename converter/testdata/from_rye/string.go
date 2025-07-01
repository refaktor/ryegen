var typeLookup = map[string]map[string]string{}
func conv_string_fromRye(ps *_env.ProgramState, obj _env.Object) (string, error) {
	if x, ok := obj.(_env.String); ok {
		return x.Value, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(string); ok {
			return v, nil
		}
	}
	return "", _errors.New("expected string, but got " + objectType(ps, obj))
}