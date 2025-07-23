package converter

import "go/types"

// ReceiverRyeTypeName returns the name of the actual type
// when the type is used as a receiver in a Rye method.
// In other words, this function returns the string should come
// before the "//" in Rye method names.
func ReceiverRyeTypeName(t types.Type, qf types.Qualifier) string {
	recv := types.TypeString(t, qf)
	{
		under := t.Underlying()
		if _, ok := under.(*types.Pointer); !ok && !types.IsInterface(under) {
			// Non-pointer, non-interface receiver should always be a pointer.
			recv = "*" + recv
		}
	}
	// Go-native types are always wrapped with go().
	return "go(" + recv + ")"
}
