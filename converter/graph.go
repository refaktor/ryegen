// Converter dependencies can be represented as a directed graph.
// Each node represents a single converter and points to the nodes
// it depends on.

package converter

import (
	"fmt"
	"go/types"
	"html"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/refaktor/ryegen/v2/converter/typeset"
	"github.com/refaktor/ryegen/v2/digraphutils"
)

type convNode struct {
	typ        types.Type
	debugNames []string // see convInfo.debugNames
	code       []byte
	deps       []convInfo
	imports    []*types.Package
	err        error
	incomplete bool
}

type convGraph struct {
	// All valid nodes. The node's err and incomplete
	// fields are always nil and false respectively.
	nodes map[convKey]convNode
	// All nodes with errors.
	errors map[convKey]error
	// Includes incomplete and error nodes. Meant
	// for debugging/testing.
	debugNodes map[convKey]convNode
	// Initial seeds paseed into the generator.
	debugSeeds []convInfo
}

// calcNodeFunc is injected into makeConvGraph to calculate a single
// node.
//
// A fundamental assumption is that calcNodeFunc called with the same
// parameters will always yield the same results.
//
// canConvert can be used if the calcNodeFunc wants to make a decision
// about whether to use a potential dependency based on whether
// the dependency can be calculated.
// canConvert will return true if the given converter can be fully
// generated without being incomplete or giving errors. Use this
// VERY sparingly, as it's not super efficient and can add recursively
// to the call stack (although it won't recurse endlessly).
type calcNodeFunc func(ci convInfo, canConvert func(convInfo) bool) (code []byte, deps []convInfo, imports []*types.Package, err error)

func makeConvGraph(seeds []convInfo, calcNode calcNodeFunc) convGraph {
	// - A node represents a single converter with its conversion
	//   code and dependencies.
	// - Let "addNext" be the set of node keys to be calculated
	//   in the next step. Set this to seeds initially.
	// - Main graph traversal: calculate nodes in "addNext".
	//   Replace "addNext" with the dependencies of the newly
	//   calculated set of nodes. *
	// - If an error occurs in any node, that node's err field is
	//   set. The err node and all of the nodes that depend on it
	//   are marked as incomplete. This marking is completed before
	//   continuing with the main graph traversal.
	// - Repeat the "main graph traversal" step until addNext is empty.
	// - Do another full graph traversal pass starting from seeds, where only
	//   nodes not marked as incomplete are kept. This deletes incomplete
	//   nodes and orphans.
	// - Finally, return these nodes and the map of converter keys
	//   to error messages.
	//
	// * If a node reports that it depends on itself, that info is fully removed
	// 	 and ignored, since it isn't useful.
	//
	// Some examples (n = addNodes, err = error, inc = incomplete):
	// - The following examples can be found as test cases in [graph_test.go/TestMakeConvGraph/example_{1,2}].
	// - seeds={A}:
	//
	//   type A struct {X; string; *A}
	//   type X int
	//                         string          int
	//                           ^              ^ string
	//      n={X,string,*A}      |   n={int}    |   ^
	//   A  ==============>  X<--A   =======>   |   |
	//                           |              X<--A<-+
	//                           v                  |  |
	//                           *A                 v  |
	//                                              *A-+
	// - Same, but now we assume type X gives an error instead of int:
	//
	//                         string         (err)
	//                           ^              ^ string
	//      n={X,string,*A}      |   n={int}    |   ^
	//   A  ==============>  X<--A   =======>   |   |
	//                           |              X<--A<-+
	//                           v                  |  |
	//                           *A                 v  |
	//           (err)                              *A-+
	//             ^    string
	//             |      ^       n={}
	//   =====>    |      |       ===> main pass done ===> all nodes are discarded
	//           (inc)<-(inc)<-+
	//                    |    |
	//                    v    |
	//                  (inc)--+

	// Nodes with calculated data and dependencies
	nodes := map[convKey]convNode{}
	// Inverted copy of node dependencies; used to propagate incompleteness
	invDeps := map[convKey][]convKey{}
	// Error origin nodes to their errors
	errNodes := map[convKey]error{}

	// Marks the errNode and all nodes that depend on it
	// as incomplete.
	var propagateIncompleteness func(errNode convKey)
	{
		// we keep these allocated across all function calls for performance
		var addNext []convKey
		var newAddNext []convKey

		propagateIncompleteness = func(errNode convKey) {
			markIncomplete := func(key convKey) {
				n, ok := nodes[key]
				if !ok {
					panic("programmer error: makeConvGraph: propageateIncompleteness: markIncomplete: converter graph inverse dependencies must all exist")
				}
				n.incomplete = true
				nodes[key] = n
			}
			addNext = append(addNext, errNode)
			for len(addNext) > 0 {
				for _, key := range addNext {
					if nodes[key].incomplete {
						continue
					}
					markIncomplete(key)
					newAddNext = append(newAddNext, invDeps[key]...)
				}
				addNext, newAddNext = newAddNext, addNext[:0]
			}
		}
	}

	// Immediately calculates whether the given type is convertible.
	var canConvert func(convInfo) bool

	mainTraversal := func(startNodes []convInfo) {
		addNext := slices.Clone(startNodes)
		var newAddNext []convInfo
		for len(addNext) > 0 {
			for _, c := range addNext {
				if n, ok := nodes[c.key]; ok {
					n.debugNames = append(n.debugNames, c.debugNames...)
					nodes[c.key] = n
					continue
				}

				// There are 3 cases, checked in order:
				// 1. node has an error -> mark it and dependants as incomplete
				// 2. node depends on an incomplete node -> skip it and mark it and dependants as incomplete
				// 3. node is OK
				// In all cases, *a* node gets added. It is only complete in case 3.

				code, deps, imports, err := calcNode(c, func(ci convInfo) bool {
					if ci.key == c.key {
						// Do not recurse on self-reference.
						return true
					}
					return canConvert(ci)
				})
				if err != nil {
					// 1. case
					nodes[c.key] = convNode{
						typ:        c.typ,
						debugNames: c.debugNames,
						err:        err,
					}
					errNodes[c.key] = err
					propagateIncompleteness(c.key)
					continue
				}

				// Remove self-references from deps
				{
					isSelfRef := func(a convInfo) bool { return a.key == c.key }
					if slices.ContainsFunc(deps, isSelfRef) {
						deps = slices.DeleteFunc(slices.Clone(deps), isSelfRef)
					}
				}

				nodes[c.key] = convNode{
					typ:        c.typ,
					debugNames: c.debugNames,
					code:       code,
					deps:       deps,
					imports:    imports,
				}

				incomplete := slices.ContainsFunc(deps, func(dep convInfo) bool {
					if depNode, ok := nodes[dep.key]; ok && depNode.incomplete {
						return true
					}
					return false
				})
				if incomplete {
					// 2. case
					propagateIncompleteness(c.key)
					continue
				}

				// 3. case
				for _, dep := range deps {
					newAddNext = append(newAddNext, dep)
					invDeps[dep.key] = append(invDeps[dep.key], c.key)
				}
			}
			addNext, newAddNext = newAddNext, addNext[:0]
		}
	}

	{
		// Used to break recursion loops: If we already
		// asked if we can convert a type and that type
		// is required to answer the question, the type
		// itself can't be the problem, so we return true.
		processing := map[convKey]bool{}

		canConvert = func(ci convInfo) bool {
			if processing[ci.key] {
				return true
			}
			processing[ci.key] = true
			defer func() {
				delete(processing, ci.key)
			}()

			mainTraversal([]convInfo{ci})
			node, ok := nodes[ci.key]
			if !ok {
				panic("programmer error: expected explicitly traversed node to exist")
			}
			return !node.incomplete
		}
	}

	// Main graph traversal (see topmost comment in this function).
	mainTraversal(seeds)

	// Clean up incomplete nodes and orphans.
	resNodes := map[convKey]convNode{}
	{
		addNext := slices.Clone(seeds)
		var newAddNext []convInfo
		for len(addNext) > 0 {
			for _, c := range addNext {
				n, ok := nodes[c.key]
				if !ok {
					panic("programmer error: makeConvGraph: expected all nodes with complete parents to be reachable")
				}
				if n.incomplete {
					continue
				}
				if _, ok := resNodes[c.key]; ok {
					continue
				}
				resNodes[c.key] = n
				newAddNext = append(newAddNext, n.deps...)
			}
			addNext, newAddNext = newAddNext, addNext[:0]
		}
	}

	return convGraph{
		nodes:      resNodes,
		errors:     errNodes,
		debugNodes: nodes,
		debugSeeds: slices.Clone(seeds),
	}
}

type GraphNode struct {
	Type types.Type
	Dir  Direction
}

// Graph represents a resulting converter graph.
// All methods will return a reasonable result
// if the *Graph is nil.
type Graph struct {
	convGraph
	typeSet    *typeset.TypeSet
	sortedKeys []convKey
}

func newGraph(cg convGraph, ts *typeset.TypeSet) *Graph {
	return &Graph{
		convGraph:  cg,
		typeSet:    ts,
		sortedKeys: slices.SortedFunc(maps.Keys(cg.nodes), convKey.cmp),
	}
}

// Nodes returns all complete and valid nodes sorted.
func (g *Graph) Nodes() iter.Seq[GraphNode] {
	return func(yield func(GraphNode) bool) {
		if g == nil {
			return
		}
		for _, key := range g.sortedKeys {
			typ := g.convGraph.nodes[key].typ
			if !yield(GraphNode{typ, key.dir}) {
				return
			}
		}
	}
}

// Contains returns whether the graph contains a complete and valid
// node with the given type and direction.
func (g *Graph) Contains(t types.Type, dir Direction) bool {
	if g == nil {
		return false
	}
	_, ok := g.nodes[convKey{typString: g.typeSet.TypeString(t), dir: dir}]
	return ok
}

// DebugDOTCode generates DOT (graphviz) code
// for the converter dependency graph.
// If nodeRe is nil, all nodes are included. If
// nodeRe is non-nil, all nodes depending on any
// matching nodes are included.
func (g *Graph) DebugDOTCode(nodeRe *regexp.Regexp) []byte {
	const graphName = "conv_graph"
	if g == nil {
		return []byte("digraph " + graphName + " {}")
	}
	seeds := g.debugSeeds
	nodes := g.debugNodes

	edges := func(k convKey) []convKey {
		node := nodes[k]
		res := make([]convKey, len(node.deps))
		for i := range node.deps {
			res[i] = node.deps[i].key
		}
		slices.SortFunc(res, convKey.cmp)
		res = slices.Compact(res)
		return res
	}

	isSeed := map[convKey]bool{}
	for _, seed := range seeds {
		isSeed[seed.key] = true
	}

	var reachable map[convKey]struct{}
	if nodeRe == nil {
		reachable = map[convKey]struct{}{}
		for k := range nodes {
			reachable[k] = struct{}{}
		}
	} else {
		var roots []convKey
		for k, n := range nodes {
			if (n.typ != nil &&
				(nodeRe.MatchString(n.typ.String()) ||
					nodeRe.MatchString(g.typeSet.TypeString(n.typ)))) ||
				slices.ContainsFunc(n.debugNames, nodeRe.MatchString) {
				roots = append(roots, k)
			}
		}

		reachable = digraphutils.Reachable(roots, edges)
	}

	return digraphutils.DOTCode(
		slices.SortedFunc(maps.Keys(reachable), convKey.cmp),
		edges,
		graphName,
		`
node[shape=box, style=filled, colorscheme=set39]
legend [label=<
  <table bgcolor="white">
    <tr><td border="0"><b>Node Types:</b></td></tr>
    <tr><td bgcolor="1">Valid, seed node</td></tr>
    <tr><td bgcolor="5">Valid</td></tr>
    <tr><td bgcolor="9">Incomplete (=depends on node with errors)</td></tr>
    <tr><td bgcolor="4">Error origin</td></tr>
  </table>
>]`,
		func(key convKey) string {
			node := nodes[key]
			var color string
			typString := node.typ.String()
			if node.err == nil {
				if node.incomplete {
					color = "9 /*incomplete*/"
				} else {
					if isSeed[key] {
						color = "1 /*valid, seed*/"
					} else {
						color = "5 /*valid*/"
					}
				}
			} else {
				color = "4 /*error origin*/"
			}
			var label strings.Builder
			fmt.Fprintf(&label, "%v", html.EscapeString(typString))
			if node.err != nil {
				fmt.Fprintf(&label, "<br/>Error: <i>%v</i>", html.EscapeString(node.err.Error()))
			}
			if len(node.debugNames) > 0 {
				fmt.Fprintf(&label, "<i>")
				for _, name := range node.debugNames {
					fmt.Fprintf(&label, "<br/>%v", html.EscapeString(name))
				}
				fmt.Fprintf(&label, "</i>")
			}
			return fmt.Sprintf("[fillcolor=%v, label=<%v: %v>]", color, key.dir, label.String())
		},
	)
}
