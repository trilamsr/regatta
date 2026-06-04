// Package cycle is the pure dependency-cycle detector used by scheduler reservation (#88).
package cycle

import (
	"errors"
	"fmt"
)

// ErrCycleDetected fires when Check would introduce or surface a cycle.
var ErrCycleDetected = errors.New("cycle: dependency cycle detected")

// Check runs Kahn's topological sort over adjacency (node -> deps) and wraps ErrCycleDetected with candidateID when any node remains undrained; dangling edges are ignored.
func Check(adjacency map[string][]string, candidateID string) error {
	n := len(adjacency)
	if n == 0 {
		return nil
	}
	idx := make(map[string]int, n)
	for id := range adjacency {
		idx[id] = len(idx)
	}
	indeg := make([]int, n)
	revHead := make([]int, n+1)
	for id, deps := range adjacency {
		i := idx[id]
		for _, dep := range deps {
			depI, ok := idx[dep]
			if !ok {
				continue
			}
			indeg[i]++
			revHead[depI+1]++
		}
	}
	for i := 1; i <= n; i++ {
		revHead[i] += revHead[i-1]
	}
	revEdges := make([]int, revHead[n])
	cursor := make([]int, n)
	for id, deps := range adjacency {
		i := idx[id]
		for _, dep := range deps {
			depI, ok := idx[dep]
			if !ok {
				continue
			}
			revEdges[revHead[depI]+cursor[depI]] = i
			cursor[depI]++
		}
	}
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			queue = append(queue, i)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, succ := range revEdges[revHead[queue[head]]:revHead[queue[head]+1]] {
			indeg[succ]--
			if indeg[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}
	if len(queue) != n {
		return fmt.Errorf("%w: %s", ErrCycleDetected, candidateID)
	}
	return nil
}
