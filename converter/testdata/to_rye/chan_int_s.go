var typeLookup = map[string]map[string]string{}
func translateChan_96c386931422030f(ps *_env.ProgramState, goCh <-chan int, ryeCh chan *_env.Object) {
    showError := func(err error) {
        ps.FailureFlag = true
        _fmt.Printf("Error from channel of type %v: %v\n", "int", err)
    }
    for {
        select {
        case v, ok := <-ryeCh:
            if !ok {
                return
            }
            _ = v
            showError(_errors.New("attempt to send to read-only Rye channel"))
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

var chanInstances_96c386931422030f_toRye_live = map[<-chan int]chan *_env.Object{}
var chanInstances_96c386931422030f_toRye_mu _sync.Mutex

func conv_chan_r_int_toRye(ps *_env.ProgramState, goCh <-chan int) (_env.Object, error) {
	if goCh == nil {
		return *_env.NewVoid(), nil
	}
	chanInstances_96c386931422030f_toRye_mu.Lock()
	ryeCh, have := chanInstances_96c386931422030f_toRye_live[goCh]
	chanInstances_96c386931422030f_toRye_mu.Unlock()
	if !have {
		ryeCh = make(chan *_env.Object)
		go func() {
			chanInstances_96c386931422030f_toRye_mu.Lock()
			chanInstances_96c386931422030f_toRye_live[goCh] = ryeCh
			chanInstances_96c386931422030f_toRye_mu.Unlock()
			translateChan_96c386931422030f(ps, goCh, ryeCh)
			chanInstances_96c386931422030f_toRye_mu.Lock()
			delete(chanInstances_96c386931422030f_toRye_live, goCh)
			chanInstances_96c386931422030f_toRye_mu.Unlock()
		}()
	}
	return *_env.NewNative(ps.Idx, ryeCh, "Rye-channel"), nil
}

func conv_int_toRye(ps *_env.ProgramState, x int) (_env.Integer, error) {
	return *_env.NewInteger(int64(x)), nil
}