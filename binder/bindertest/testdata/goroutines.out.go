res0 := testmodule.MakeChan()
var res0Obj env.Object
if res0 != nil {
	ch := make(chan *env.Object)
	go func() {
		for {
			select {
			case v, ok := <-ch:
				if !ok {
					close(res0)
					return
				}
				var ov int
				if vc, ok := (*v).(env.Integer); ok {
					ov = int(vc.Value)
				} else {
					ps.FailureFlag = true
					fmt.Printf("\033[31mError: \033[1m%v\033[m\n",
						"((RYEGEN:FUNCNAME)): arg 0: channel object: "+"expected integer, but got "+objectDebugString(ps.Idx, (*v)),
					)
					return
				}
				res0 <- ov
			case v, ok := <-res0:
				if !ok {
					close(ch)
					return
				}
				var ov env.Object
				ov = *env.NewInteger(int64(v))
				ch <- &ov
			}
		}
	}()
	res0Obj = *env.NewNative(ps.Idx, ch, "Rye-channel")
}
return res0Obj

//================================//

var arg0Val chan int
switch v := arg0.(type) {
case env.Native:
	ch, ok := v.Value.(chan *env.Object)
	if !ok {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected Rye-channel (native of type chan *env.Object) or nil, but got "+objectDebugString(ps.Idx, arg0))
	}
	go func() {
		for {
			select {
			case v, ok := <-ch:
				if !ok {
					close(arg0Val)
					return
				}
				var ov int
				if vc, ok := (*v).(env.Integer); ok {
					ov = int(vc.Value)
				} else {
					ps.FailureFlag = true
					fmt.Printf("\033[31mError: \033[1m%v\033[m\n",
						"((RYEGEN:FUNCNAME)): arg 1: channel object: "+"expected integer, but got "+objectDebugString(ps.Idx, (*v)),
					)
					return
				}
				arg0Val <- ov
			case v, ok := <-arg0Val:
				if !ok {
					close(ch)
					return
				}
				var ov env.Object
				ov = *env.NewInteger(int64(v))
				ch <- &ov
			}
		}
	}()
case env.Integer:
	if v.Value != 0 {
		ps.FailureFlag = true
		return env.NewError("((RYEGEN:FUNCNAME)): arg 1: "+"expected integer to be 0 or nil, but got "+strconv.FormatInt(v.Value, 10))
	}
	arg0Val = nil
}
testmodule.UseChan(arg0Val)
return nil
