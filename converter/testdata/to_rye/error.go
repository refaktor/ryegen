var typeLookup = map[string]map[string]string{}
func init() {
	typeLookup[""] = map[string]string{}
	typeLookup[""]["error"] = "error"
}

func conv_error_toRye(ps *_env.ProgramState, x error) (_env.Object, error) {
	if x == nil {
		return *_env.NewVoid(), nil
	}
	return _env.NewError(x.Error()), nil
}