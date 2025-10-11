var typeLookup = map[string]map[string]string{}
func init() {
	typeLookup[""] = map[string]string{}
	typeLookup[""]["error"] = "error"
}

func conv_error_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, s error) (_env.Object, error) {
	if s == nil {
		return *_env.NewVoid(), nil
	}
	if nat, ok := autoToNative(ps, s); ok {
		return nat, nil
	}
	return *_env.NewNative(ps.Idx, s, "go(error)"), nil
}