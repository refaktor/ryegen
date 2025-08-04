package converter

import (
	"cmp"
	"go/types"
	"slices"

	"github.com/refaktor/ryegen/v2/converter/typeset"
)

// ReceiverRyeTypeName returns the name of the actual type
// when the type is used as a receiver in a Rye method.
// In other words, this function returns the string should come
// before the "//" in Rye method names.
func ReceiverRyeTypeName(t types.Type, tset *typeset.TypeSet) string {
	recv := tset.TypeString(t)
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

func cmpPkgs(a, b *types.Package) int {
	switch {
	case a == b:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	return cmp.Compare(a.Path(), b.Path())
}

// sortedUniq sorts s before removing all duplicates.
func sortedUniq[T any, S ~[]T](s S, compare func(T, T) int) S {
	slices.SortFunc(s, compare)
	return slices.CompactFunc(s, func(a, b T) bool {
		return compare(a, b) == 0
	})
}
