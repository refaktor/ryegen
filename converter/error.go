package converter

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

type ConverterError struct {
	errors     map[convKey]error
	validNodes map[convKey]struct{}
}

// Returns nil if the graph had no errors.
// Underlying type is always [*ConverterError].
func newConverterError(graph convGraph) error {
	if len(graph.errors) == 0 {
		return nil
	}

	validNodes := make(map[convKey]struct{}, len(graph.nodes))
	for k := range graph.nodes {
		validNodes[k] = struct{}{}
	}

	return &ConverterError{errors: graph.errors, validNodes: validNodes}
}

func (e *ConverterError) firstKey() convKey {
	if len(e.errors) == 0 {
		panic("programmer error: Error.firstKey called with no errors")
	}
	var smallest convKey
	isFirst := true
	for k := range e.errors {
		if isFirst {
			smallest = k
			continue
		}
		if k.cmp(smallest) < 0 {
			smallest = k
		}
	}
	return smallest
}

func (e *ConverterError) sortedKeys() []convKey {
	return slices.SortedFunc(maps.Keys(e.errors), convKey.cmp)
}

func (e *ConverterError) printSingleMessage(w io.Writer, k convKey) {
	dirStr := "to Rye"
	if k.dir == FromRye {
		dirStr = "from Rye"
	}
	fmt.Fprintf(w, "convert %v %v: %v", k.typString, dirStr, e.errors[k])
}

// Error returns a short error message.
func (e *ConverterError) Error() string {
	if len(e.errors) == 0 {
		return "success"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%v converter errors, first: ", len(e.errors))
	e.printSingleMessage(&b, e.firstKey())
	return b.String()
}

// String returns a full multi-line error message containing
// all errors.
func (e *ConverterError) String() string {
	var b strings.Builder
	for _, k := range e.sortedKeys() {
		e.printSingleMessage(&b, k)
		b.WriteByte('\n')
	}
	return b.String()
}

func (e *ConverterError) Unwrap() []error {
	if len(e.errors) == 0 {
		return nil
	}

	keys := e.sortedKeys()
	errs := make([]error, 0, len(keys))
	for _, k := range keys {
		errs = append(errs, e.errors[k])
	}
	return errs
}
