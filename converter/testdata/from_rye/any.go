var typeLookup = map[string]map[string]string{}
func conv_unk_e6f7b419052023cd_fromRye(ps *_env.ProgramState, obj _env.Object) (any, error) {
	if nat, ok := obj.(_env.Native); ok {
		return nat.Value, nil
	}
	return nil, _errors.New("expected Native, but got " + objectType(ps, obj))
}