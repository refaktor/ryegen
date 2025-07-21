package converter

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func printGraph(g convGraph) string {
	compareConvKeys := func(a, b convKey) int {
		return cmp.Or(
			cmp.Compare(a.dir, b.dir),
			cmp.Compare(a.typString, b.typString),
		)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Nodes:")
	if len(g.nodes) == 0 {
		fmt.Fprintf(&b, " <nil>")
	}
	fmt.Fprintf(&b, "\n")
	for _, k := range slices.SortedFunc(maps.Keys(g.nodes), compareConvKeys) {
		n := g.nodes[k]
		fmt.Fprintf(&b, "  %v -> {", k.typString)
		for i, dep := range n.deps {
			if i != 0 {
				fmt.Fprintf(&b, ", ")
			}
			fmt.Fprintf(&b, "%v", dep.key.typString)
		}
		fmt.Fprintf(&b, "}\n")
	}

	fmt.Fprintf(&b, "Errors:")
	if len(g.errors) == 0 {
		fmt.Fprintf(&b, " <nil>")
	}
	fmt.Fprintf(&b, "\n")
	for _, k := range slices.SortedFunc(maps.Keys(g.errors), compareConvKeys) {
		err := g.errors[k]
		fmt.Fprintf(&b, "  %v: %v\n", k.typString, err)
	}
	return b.String()
}

type rule struct {
	name string
	deps []string
	err  error
}

// Returns the func that actually runs makeConvGraph so performance
// can be measured more accurately.
func graphBuilder(seeds []string, rules []rule) (run func() (_ convGraph, visitedNodes int)) {
	var seedsCI []convInfo
	for _, s := range seeds {
		seedsCI = append(seedsCI, convInfo{key: convKey{typString: s}})
	}
	rulesM := map[string]rule{}
	for _, r := range rules {
		rulesM[r.name] = r
	}
	return func() (_ convGraph, visitedNodes int) {
		graph := makeConvGraph(seedsCI, func(c convInfo, canConv func(convInfo) bool) (code []byte, deps []convInfo, importPaths []string, err error) {
			visitedNodes++
			if r, ok := rulesM[c.key.typString]; ok {
				if r.err != nil {
					return nil, nil, nil, r.err
				}
				var depsCI []convInfo
				for _, dep := range r.deps {
					depsCI = append(depsCI, convInfo{key: convKey{typString: dep}})
				}
				return nil, depsCI, nil, nil
			}
			return nil, nil, nil, nil
		})
		return graph, visitedNodes
	}
}

type randomGraphOpts struct {
	Nodes            int     // number of total nodes
	Seeds            int     // number of seeds
	DepProbability   float32 // probability that a node gets any dependencies
	MinDeps          int     // minimum number of dependenciess if a node gets any
	MaxDeps          int     // maximum number of dependenciess if a node gets any
	ErrorProbability float32 // probability that any error nodes exist
	MinErrors        int     // miminum number of errors if any
	MaxErrors        int     // maximum number of errors if any
}

func (opts randomGraphOpts) generate(randSrc rand.Source) (seeds []string, rules []rule) {
	rng := rand.New(randSrc)

	nodeNames := make([]string, opts.Nodes)
	for i := range opts.Nodes {
		nodeNames[i] = strconv.Itoa(i)
	}

	for _, i := range rng.Perm(opts.Nodes)[:opts.Seeds] {
		seeds = append(seeds, nodeNames[i])
	}

	rules = make([]rule, opts.Nodes)
	for i := range opts.Nodes {
		rules[i].name = nodeNames[i]
		if rng.Float32() < opts.DepProbability {
			numDeps := opts.MinDeps + rng.Intn(opts.MaxDeps-opts.MinDeps+1)
			for range numDeps {
				rules[i].deps = append(rules[i].deps,
					nodeNames[rng.Intn(opts.Nodes)])
			}
		}
	}

	if rng.Float32() < opts.ErrorProbability {
		numErrors := opts.MinErrors + rng.Intn(opts.MaxErrors-opts.MinErrors+1)
		for _, i := range rng.Perm(opts.Nodes)[:numErrors] {
			rules[i].err = errors.New("error")
		}
	}
	return
}

func TestMakeConvGraph(t *testing.T) {
	runTest := func(name string, seeds []string, rules []rule, expect string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			graph, _ := graphBuilder(seeds, rules)()
			require.Equal(t,
				strings.TrimSpace(expect),
				strings.TrimSpace(printGraph(graph)),
			)
		})
	}

	runTest("example_1",
		[]string{"A"},
		[]rule{
			{name: "A", deps: []string{"X", "string", "*A"}},
			{name: "*A", deps: []string{"A"}},
			{name: "X", deps: []string{"int"}},
		},
		`
Nodes:
  *A -> {A}
  A -> {X, string, *A}
  X -> {int}
  int -> {}
  string -> {}
Errors: <nil>
`,
	)

	runTest("example_2",
		[]string{"A"},
		[]rule{
			{name: "A", deps: []string{"X", "string", "*A"}},
			{name: "*A", deps: []string{"A"}},
			{name: "X", deps: []string{"int"}},
			{name: "int", err: errors.New("test")},
		},
		`
Nodes: <nil>
Errors:
  int: test
`,
	)

	runTest("self_reference",
		[]string{"A"},
		[]rule{
			{name: "A", deps: []string{"A", "string"}},
		},
		`
Nodes:
  A -> {string}
  string -> {}
Errors: <nil>
`,
	)
}

func FuzzMakeConvGraph(f *testing.F) {
	f.Add(int64(0))
	f.Fuzz(func(t *testing.T, seed int64) {
		t.Parallel()

		require := require.New(t)

		seedNodes, rules := (randomGraphOpts{
			Nodes:            1000,
			Seeds:            100,
			DepProbability:   0.7,
			MinDeps:          1,
			MaxDeps:          4,
			ErrorProbability: 0.8,
			MinErrors:        1,
			MaxErrors:        4,
		}).generate(rand.NewSource(seed))

		graph, _ := graphBuilder(seedNodes, rules)()

		isNode := func(key convKey) bool {
			_, ok := graph.nodes[key]
			return ok
		}
		isSeed := func(key convKey) bool {
			for _, s := range seedNodes {
				if s == key.typString {
					return true
				}
			}
			return false
		}
		isDependencyOfAny := func(key convKey) bool {
			for _, node := range graph.nodes {
				for _, dep := range node.deps {
					if dep.key == key {
						return true
					}
				}
			}
			return false
		}
		for key, node := range graph.nodes {
			require.True(isSeed(key) || isDependencyOfAny(key), "every node must be either a seed or depended on by some other node")
			require.False(node.incomplete, "all final nodes must be complete")
			require.Nil(node.err, "all final nodes must have no errors")
		}
		for key, err := range graph.errors {
			require.True(!isNode(key) && !isDependencyOfAny(key), "no error node may be anywhere in the resulting graph")
			require.NotNil(err, "all error nodes must have an error")
		}
		for _, seed := range seedNodes {
			key := convKey{typString: seed}
			debugNode, ok := graph.debugNodes[key]
			require.True(ok, "all seeds must be present in the unpruned graph")
			require.True(debugNode.incomplete || isNode(key), "all non-incomplete seeds must be present in the resulting graph")
		}
	})
}

func BenchmarkMakeConvGraph(b *testing.B) {
	var prunedNodes int
	var totalVisitedNodes int
	i := int64(0)
	for b.Loop() {
		b.StopTimer()
		opts := randomGraphOpts{
			Nodes:            1000,
			Seeds:            100,
			DepProbability:   0.7,
			MinDeps:          1,
			MaxDeps:          4,
			ErrorProbability: 0.4,
			MinErrors:        1,
			MaxErrors:        4,
		}
		seedNodes, rules := opts.generate(rand.NewSource(i))
		buildGraph := graphBuilder(seedNodes, rules)
		b.StartTimer()
		graph, visitedNodes := buildGraph()
		prunedNodes += opts.Nodes - len(graph.nodes)
		totalVisitedNodes += visitedNodes
		i++
	}
	b.ReportMetric(float64(prunedNodes)/float64(b.N), "pruned_nodes/op")
	b.ReportMetric(float64(totalVisitedNodes)/float64(b.N), "visited_nodes/op")
}
