/*
Package typeset handles type stringification and automatic struct aliasing.

A type is "normalized" when all of its previous aliases are resolved,
function signature receivers are moved into the first function parameter and
all inline struct declarations are replaced with generated aliases.

Normalized types are always assignable to their unnormalized counterparts
and vice versa.

[TypeSet] allows handling all this in a cached manner.
*/
package typeset

import (
	"go/types"
	"iter"
	"maps"
	"slices"
)

// TypeSet handles stringification and automatic aliasing of types.
//
// Note that TypeSet includes any function receivers in
// its stringification (which differs from Go's types package).
//
// TypeSet also automatically creates aliases for struct types
// so that their strings are kept small. You can query all
// created aliases with [TypeSet.Aliases].
type TypeSet struct {
	qualifier types.Qualifier
	nameCache map[types.Type]string     // unnormalized type to normalized string
	normCache map[types.Type]types.Type // unnormalized type to normalized
	aliases   map[string]*types.Struct  // alias name to underlying struct
}

// New creates a new [TypeSet].
func New(qualifier types.Qualifier) *TypeSet {
	return &TypeSet{
		qualifier: qualifier,
		nameCache: map[types.Type]string{},
		normCache: map[types.Type]types.Type{},
		aliases:   map[string]*types.Struct{},
	}
}

// TypeString returns the qualified string for the type after normalization.
// The string and normalized type are cached for future calls,
// and any aliases required are registered.
func (ts *TypeSet) TypeString(t types.Type) string {
	if s, ok := ts.nameCache[t]; ok {
		return s
	}

	ts.normalizeAndAddType(t)

	s, ok := ts.nameCache[t]
	if !ok {
		panic("programmer error: type should have been added to name cache")
	}
	return s
}

// ContainsAlias returns true if the given type is a struct
// alias within this type set.
// If ContainsAlias returned true, t is guaranteed to be of type
// *types.Alias.
func (ts *TypeSet) ContainsAlias(t types.Type) bool {
	if _, ok := t.(*types.Alias); !ok {
		return false
	}
	s := ts.TypeString(t)
	_, ok := ts.aliases[s]
	return ok
}

// Normalized returns the normalized version of typ.
// Cached for future calls.
func (ts *TypeSet) Normalized(typ types.Type) types.Type {
	if t, ok := ts.normCache[typ]; ok {
		return t
	}

	return ts.normalizeAndAddType(typ)
}

// Qualifier returns the qualifier passed
// at initialization.
func (ts *TypeSet) Qualifier() types.Qualifier { return ts.qualifier }

type Alias struct {
	Name string
	Type *types.Struct
}

// Aliases returns all aliased structs, sorted by name.
// Use types.TypeString(Alias.Type, TypeSet.Qualifier) to
// obtain the code for the referenced struct.
func (ts *TypeSet) Aliases() iter.Seq[Alias] {
	return func(yield func(Alias) bool) {
		keys := slices.Sorted(maps.Keys(ts.aliases))
		for _, key := range keys {
			if !yield(Alias{key, ts.aliases[key]}) {
				break
			}
		}
	}
}
