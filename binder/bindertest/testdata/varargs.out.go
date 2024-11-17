type iface_testmodule_Example struct {
	self env.RyeCtx
	fn_MyFn func(self env.RyeCtx, arg0 ...string)
	fn_Unused func(self env.RyeCtx, arg0 int)
}

func (self *iface_testmodule_Example) MyFn(arg0 ...string) {
	self.fn_MyFn(self.self, arg0)
}

func (self *iface_testmodule_Example) Unused(arg0 int) {
	self.fn_Unused(self.self, arg0)
}

func ctxTo_testmodule_Example(ps *env.ProgramState, v env.RyeCtx) (testmodule.Example, error) {
	words := v.GetWords(*ps.Idx).Series.S
	wordToObj := make(map[string]env.Object, len(words))
	for _, word := range words {
		name := word.(env.String).Value
		idx, ok := ps.Idx.GetIndex(name)
		if !ok {
			panic("expected valid word")
		}
		obj, ok := v.Get(idx)
		if !ok {
			panic("expected valid index")
		}
		wordToObj[name] = obj
	}
	impl := &iface_testmodule_Example{
		self: v,
	}
	ctxObj0, ok := wordToObj["my-fn"]
	if !ok {
		return nil, errors.New("context to testmodule.Example: expected context to have function MyFn")
	}
	switch fn := ctxObj0.(type) {
	case env.Function:
		if fn.Argsn != 1 {
			return nil, errors.New("context to testmodule.Example: context fn MyFn: "+"expected 1 function arguments, but got "+strconv.Itoa(fn.Argsn))
		}
		impl.fn_MyFn = func(ctx env.RyeCtx, farg0 ...string) {
			var farg0Val env.Object
			{
				items := make([]env.Object, len(farg0))
				for i, it := range farg0 {
					items[i] = *env.NewString(it)
				}
				farg0Val = *env.NewBlock(*env.NewTSeries(items))
			}
			actualFn := fn
			_ = actualFn
			evaldo.CallFunctionArgsN(fn, ps, &ctx, farg0Val)
		}
	default:
		return nil, errors.New("context to testmodule.Example: context fn MyFn: "+"expected function, but got "+objectDebugString(ps.Idx, fn))
	}
	ctxObj1, ok := wordToObj["unused"]
	if !ok {
		return nil, errors.New("context to testmodule.Example: expected context to have function Unused")
	}
	switch fn := ctxObj1.(type) {
	case env.Function:
		if fn.Argsn != 1 {
			return nil, errors.New("context to testmodule.Example: context fn Unused: "+"expected 1 function arguments, but got "+strconv.Itoa(fn.Argsn))
		}
		impl.fn_Unused = func(ctx env.RyeCtx, farg0 int) {
			var farg0Val env.Object
			farg0Val = *env.NewInteger(int64(farg0))
			actualFn := fn
			_ = actualFn
			evaldo.CallFunctionArgsN(fn, ps, &ctx, farg0Val)
		}
	default:
		return nil, errors.New("context to testmodule.Example: context fn Unused: "+"expected function, but got "+objectDebugString(ps.Idx, fn))
	}
	return impl, nil
}


//================================//

var arg0Val testmodule.Example
switch v := arg0.(type) {
case env.RyeCtx:
	var err error
	arg0Val, err = ctxTo_testmodule_Example(ps, v)
	if err != nil {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+err.Error())
	}
case env.Native:
	if vc, ok := v.Value.(testmodule.Example); ok {
		arg0Val = vc
	} else {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected native of type testmodule.Example, but got "+objectDebugString(ps.Idx, v))
	}
case env.Integer:
	if v.Value != 0 {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected integer to be 0 or nil, but got "+strconv.FormatInt(v.Value, 10))
	}
	arg0Val = nil
default:
	ps.FailureFlag = true
	return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected native, but got "+objectDebugString(ps.Idx, v))
}
testmodule.DoSomething(arg0Val)
return nil

//================================//

var arg0Val func(...any)
switch fn := arg0.(type) {
case env.Function:
	if fn.Argsn != 1 {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected 1 function arguments, but got "+strconv.Itoa(fn.Argsn))
	}
	arg0Val = func(farg0 ...any) {
		var farg0Val env.Object
		{
			items := make([]env.Object, len(farg0))
			for i, it := range farg0 {
				items[i] = *env.NewNative(ps.Idx, it, "Go(any)")
			}
			farg0Val = *env.NewBlock(*env.NewTSeries(items))
		}
		actualFn := fn
		_ = actualFn
		evaldo.CallFunctionArgsN(fn, ps, ps.Ctx, farg0Val)
	}
case env.Integer:
	if fn.Value != 0 {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected integer to be 0 or nil, but got "+strconv.FormatInt(fn.Value, 10))
	}
	arg0Val = nil
default:
	ps.FailureFlag = true
	return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected function or nil, but got "+objectDebugString(ps.Idx, fn))
}
testmodule.Functor(arg0Val)
return nil
