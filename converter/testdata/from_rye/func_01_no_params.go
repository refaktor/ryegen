func conv_func_1926bfa0a15a6c3c_fromRye(ps *_env.ProgramState, obj _env.Object) (func(), error) {
	if fn, ok := obj.(_env.Function); ok {
		if fn.Argsn != 0 {
			return nil, _errors.New("expected function with 0 args, but got " + objectType(ps, obj))
		}
		return func() {
			_evaldo.CallFunctionArgsN(fn, ps, ps.Ctx)
			if e, ok := ps.Res.(*_env.Error); ok {
				showFunctionError(ps, fn, _errors.New(e.Message))
				return
			}
		}, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(func()); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected function or native of type go(func()), but got " + objectType(ps, obj))
}