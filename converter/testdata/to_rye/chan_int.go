var typeLookup = map[string]map[string]string{}
func translateChan_202e8713191152c4(ps *_env.ProgramState, goCh chan int, ryeCh chan *_env.Object) {
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
        case v, ok := <-goCh:
            if !ok {
                close(ryeCh)
                return
            }
            ov, err := conv_int_toRye(ps, v)
            if err != nil {
                showError(err)
                continue
            }
            ovObj := _env.Object(ov)
            ryeCh <- &ovObj
        }
    }
}

var chanInstances_202e8713191152c4_toRye_live = map[chan int]chan *_env.Object{}
var chanInstances_202e8713191152c4_toRye_mu _sync.Mutex

func conv_chan_sr_int_toRye(ps *_env.ProgramState, goCh chan int) (_env.Object, error) {
	if goCh == nil {
		return *_env.NewVoid(), nil
	}
	chanInstances_202e8713191152c4_toRye_mu.Lock()
	ryeCh, have := chanInstances_202e8713191152c4_toRye_live[goCh]
	chanInstances_202e8713191152c4_toRye_mu.Unlock()
	if !have {
		ryeCh = make(chan *_env.Object)
		go func() {
			chanInstances_202e8713191152c4_toRye_mu.Lock()
			chanInstances_202e8713191152c4_toRye_live[goCh] = ryeCh
			chanInstances_202e8713191152c4_toRye_mu.Unlock()
			translateChan_202e8713191152c4(ps, goCh, ryeCh)
			chanInstances_202e8713191152c4_toRye_mu.Lock()
			delete(chanInstances_202e8713191152c4_toRye_live, goCh)
			chanInstances_202e8713191152c4_toRye_mu.Unlock()
		}()
	}
	return *_env.NewNative(ps.Idx, ryeCh, "Rye-channel"), nil
}

func conv_int_toRye(ps *_env.ProgramState, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
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