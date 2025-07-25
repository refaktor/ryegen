package pkgutils

import "strings"

// Returns true if s is a package or module path in
// the std library, i.e. the first element contains
// no dot.
// Doesn't actually check if the std library package
// exists.
// Returns false if s is empty.
func IsPkgPathStd(s string) bool {
	firstElem, _, _ := strings.Cut(s, "/")
	return !strings.Contains(firstElem, ".")
}
