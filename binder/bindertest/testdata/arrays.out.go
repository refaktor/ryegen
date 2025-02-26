var arg0Val []string
switch v := arg0.(type) {
case env.Block:
	arg0Val = make([]string, len(v.Series.S))
	for i, it := range v.Series.S {
		iv := &arg0Val[i]
		if vc, ok := it.(env.String); ok {
			(*iv) = string(vc.Value)
		} else {
			ps.FailureFlag = true
			return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"block item: "+"expected string, but got "+objectDebugString(ps.Idx, it))
		}
	}
case env.Integer:
	if v.Value != 0 {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected integer to be 0 or nil, but got "+strconv.FormatInt(v.Value, 10))
	}
	arg0Val = nil
default:
	ps.FailureFlag = true
	return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected block or nil, but got "+objectDebugString(ps.Idx, v))
}
testmodule.ProcessSlice(arg0Val)
return nil

//================================//

var arg0Val [][]string
switch v := arg0.(type) {
case env.Block:
	arg0Val = make([][]string, len(v.Series.S))
	for i, it := range v.Series.S {
		iv := &arg0Val[i]
		switch v := it.(type) {
		case env.Block:
			(*iv) = make([]string, len(v.Series.S))
			for i, it := range v.Series.S {
				iv := &(*iv)[i]
				if vc, ok := it.(env.String); ok {
					(*iv) = string(vc.Value)
				} else {
					ps.FailureFlag = true
					return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"block item: "+"block item: "+"expected string, but got "+objectDebugString(ps.Idx, it))
				}
			}
		case env.Integer:
			if v.Value != 0 {
				ps.FailureFlag = true
				return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"block item: "+"expected integer to be 0 or nil, but got "+strconv.FormatInt(v.Value, 10))
			}
			(*iv) = nil
		default:
			ps.FailureFlag = true
			return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"block item: "+"expected block or nil, but got "+objectDebugString(ps.Idx, v))
		}
	}
case env.Integer:
	if v.Value != 0 {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected integer to be 0 or nil, but got "+strconv.FormatInt(v.Value, 10))
	}
	arg0Val = nil
default:
	ps.FailureFlag = true
	return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected block or nil, but got "+objectDebugString(ps.Idx, v))
}
testmodule.ProcessSliceSlice(arg0Val)
return nil
