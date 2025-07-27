// Package digraphutils provides utilities for directed graphs, represented as
// a mapping from node keys to edges.
package digraphutils

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/refaktor/ryegen/v2/textutils"
)

func Reachable[K comparable](roots []K, edges func(K) []K) map[K]struct{} {
	reachable := map[K]struct{}{}
	nodes := slices.Clone(roots)
	var newNodes []K
	for len(nodes) > 0 {
		for _, node := range nodes {
			if _, ok := reachable[node]; ok {
				continue
			}
			reachable[node] = struct{}{}
			newNodes = append(newNodes, edges(node)...)
		}
		nodes, newNodes = newNodes, nodes[:0]
	}
	return reachable
}

// DOTCode generates graphviz DOT code to visualize a graph.
// nodes represents all nodes included in the graph.
// name is the name of the digraph, prelude DOT code inserted
// in the beginning, and nodeAttrs should return a string representing
// a node's attributes (in []).
func DOTCode[K comparable](nodes []K, edges func(K) []K, name, prelude string, nodeAttrs func(K) string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "digraph %v {\n", name)
	b.WriteString(textutils.IndentString(strings.TrimSpace(prelude), "  ", 1))
	b.WriteByte('\n')
	nodeIDs := map[K]int{}
	for id, key := range nodes {
		fmt.Fprintf(&b, "  %v", id)
		if attrs := nodeAttrs(key); attrs != "" {
			b.WriteByte(' ')
			b.WriteString(attrs)
		}
		b.WriteByte('\n')
		nodeIDs[key] = id
	}
	for id, key := range nodes {
		edgs := edges(key)
		edgs = slices.DeleteFunc(edgs, func(k K) bool {
			_, ok := nodeIDs[key]
			return !ok
		})
		if len(edgs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %v -> {", id)
		for i, edg := range edgs {
			if i != 0 {
				b.WriteByte(' ')
			}
			fmt.Fprintf(&b, "%v", nodeIDs[edg])
		}
		fmt.Fprintf(&b, "}\n")
	}
	fmt.Fprintf(&b, "}\n")
	return b.Bytes()
}
