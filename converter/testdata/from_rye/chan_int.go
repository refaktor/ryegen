var pkgLookup = make(map[string]string, 0)
func conv_int_toRye(ps *_env.ProgramState, ctx *_env.RyeCtx, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}

func translateChan_202e8713191152c4(ps *_env.ProgramState, ctx *_env.RyeCtx, goCh chan int, ryeCh chan *_env.Object) {
    showError := func(err error) {
        ps.FailureFlag = true
        _fmt.Printf("Error from channel of type %v: %v\n", "int", err)
    }
    for {
        select {
        case v, ok := <-ryeCh:
            if !ok {
                close(goCh)
                return
            }
            ov, err := conv_int_fromRye(ps, ctx, *v)
            if err != nil {
                showError(err)
                continue
            }
            goCh <- ov
        case v, ok := <-goCh:
            if !ok {
                close(ryeCh)
                return
            }
            ov, err := conv_int_toRye(ps, ctx, v)
            if err != nil {
                showError(err)
                continue
            }
            ovObj := _env.Object(ov)
            ryeCh <- &ovObj
        }
    }
}

var chanInstances_202e8713191152c4_fromRye_live = map[chan *_env.Object]chan int{}
var chanInstances_202e8713191152c4_fromRye_mu _sync.Mutex

func conv_chan_sr_int_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (chan int, error) {
	if isNil(obj) {
		return nil, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if ryeCh, ok := nat.Value.(chan *_env.Object); ok {
			chanInstances_202e8713191152c4_fromRye_mu.Lock()
			goCh, have := chanInstances_202e8713191152c4_fromRye_live[ryeCh]
			chanInstances_202e8713191152c4_fromRye_mu.Unlock()
			if !have {
				goCh = make(chan int)
				go func() {
					chanInstances_202e8713191152c4_fromRye_mu.Lock()
					chanInstances_202e8713191152c4_fromRye_live[ryeCh] = goCh
					chanInstances_202e8713191152c4_fromRye_mu.Unlock()
					translateChan_202e8713191152c4(ps, ctx, goCh, ryeCh)
					chanInstances_202e8713191152c4_fromRye_mu.Lock()
					delete(chanInstances_202e8713191152c4_fromRye_live, ryeCh)
					chanInstances_202e8713191152c4_fromRye_mu.Unlock()
				}()
			}
			return goCh, nil
		}
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(chan int); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected channel of type " + "int" + ", but got " + objectType(ps, obj))
}

func conv_int_fromRye(ps *_env.ProgramState, ctx *_env.RyeCtx, obj _env.Object) (int, error) {
	if x, ok := obj.(_env.Integer); ok {
		return int(x.Value), nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(int); ok {
			return v, nil
		}
	}
	return 0, _errors.New("expected int, but got " + objectType(ps, obj))
}