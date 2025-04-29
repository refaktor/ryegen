func conv_error_toRye(ps *_env.ProgramState, x error) (*_env.Error, error) {
	return _env.NewError(x.Error()), nil
}