var typeLookup = map[string]map[string]string{}
func translateChan_f0ab6014f231c82b(ps *_env.ProgramState, goCh chan<- int, ryeCh chan *_env.Object) {
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
            ov, err := conv_int_fromRye(ps, *v)
            if err != nil {
                showError(err)
                continue
            }
            goCh <- ov
        }
    }
}

var chanInstances_96c386931422030f_fromRye_live = map[chan *_env.Object]chan int{}
var chanInstances_96c386931422030f_fromRye_mu _sync.Mutex

func conv_chan_r_int_fromRye(ps *_env.ProgramState, obj _env.Object) (<-chan int, error) {
	if isNil(obj) {
		return nil, nil
	}
	if nat, ok := obj.(_env.Native); ok {
		if ryeCh, ok := nat.Value.(chan *_env.Object); ok {
			chanInstances_96c386931422030f_fromRye_mu.Lock()
			goCh, have := chanInstances_96c386931422030f_fromRye_live[ryeCh]
			chanInstances_96c386931422030f_fromRye_mu.Unlock()
			if !have {
				goCh = make(chan int)
				go func() {
					chanInstances_96c386931422030f_fromRye_mu.Lock()
					chanInstances_96c386931422030f_fromRye_live[ryeCh] = goCh
					chanInstances_96c386931422030f_fromRye_mu.Unlock()
					translateChan_f0ab6014f231c82b(ps, goCh, ryeCh)
					chanInstances_96c386931422030f_fromRye_mu.Lock()
					delete(chanInstances_96c386931422030f_fromRye_live, ryeCh)
					chanInstances_96c386931422030f_fromRye_mu.Unlock()
				}()
			}
			return goCh, nil
		}
	}
	if nat, ok := obj.(_env.Native); ok {
		if v, ok := nat.Value.(<-chan int); ok {
			return v, nil
		}
	}
	return nil, _errors.New("expected channel of type " + "int" + ", but got " + objectType(ps, obj))
}

func conv_int_fromRye(ps *_env.ProgramState, obj _env.Object) (int, error) {
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