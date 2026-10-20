//go:build unit

package service

import (
	"sort"
	"testing"
)

func codingAlgorithmOracle(t *testing.T, item ModelBenchmarkItem) any {
	t.Helper()
	switch item.FamilyID {
	case "negative-cycle-influence":
		d := codingQuestion[benchmarkCodingGraph](t, item)
		const inf = 1 << 25
		dist := make([][]int, d.Nodes)
		for i := range dist {
			dist[i] = make([]int, d.Nodes)
			for j := range dist[i] {
				if i != j {
					dist[i][j] = inf
				}
			}
		}
		for _, edge := range d.Edges {
			dist[edge.From][edge.To] = min(dist[edge.From][edge.To], edge.Cost)
		}
		for k := 0; k < d.Nodes; k++ {
			for i := 0; i < d.Nodes; i++ {
				for j := 0; j < d.Nodes; j++ {
					if dist[i][k] != inf && dist[k][j] != inf {
						dist[i][j] = min(dist[i][j], dist[i][k]+dist[k][j])
					}
				}
			}
		}
		out := make([]any, d.Nodes)
		for node := range out {
			out[node] = dist[0][node]
			if dist[0][node] == inf {
				out[node] = "unreachable"
			}
			for cycle := 0; cycle < d.Nodes; cycle++ {
				if dist[0][cycle] != inf && dist[cycle][cycle] < 0 && dist[cycle][node] != inf {
					out[node] = "-inf"
				}
			}
		}
		return out
	case "maxflow-mincut":
		d := codingQuestion[benchmarkCodingGraph](t, item)
		best, count := 1<<25, 0
		for middle := 0; middle < 1<<(d.Nodes-2); middle++ {
			set := make([]bool, d.Nodes)
			set[0] = true
			for i := 1; i < d.Nodes-1; i++ {
				set[i] = middle>>(i-1)&1 == 1
			}
			capacity := 0
			for _, edge := range d.Edges {
				if set[edge.From] && !set[edge.To] {
					capacity += edge.Cost
				}
			}
			if capacity < best {
				best, count = capacity, 1
			} else if capacity == best {
				count++
			}
		}
		return map[string]any{"flow": best, "cut_count": count}
	case "bipartite-matching":
		d := codingQuestion[benchmarkMatchingData](t, item)
		best, count := -1, 0
		used := make([]bool, d.Right)
		var assign func(int, int)
		assign = func(left, size int) {
			if left == d.Left {
				if size > best {
					best, count = size, 1
				} else if size == best {
					count++
				}
				return
			}
			assign(left+1, size)
			for _, edge := range d.Edges {
				if edge[0] == left && !used[edge[1]] {
					used[edge[1]] = true
					assign(left+1, size+1)
					used[edge[1]] = false
				}
			}
		}
		assign(0, 0)
		return map[string]any{"size": best, "count": count}
	case "scc-condensation":
		d := codingQuestion[benchmarkCodingGraph](t, item)
		adj, reverse := make([][]int, d.Nodes), make([][]int, d.Nodes)
		for _, edge := range d.Edges {
			adj[edge.From] = append(adj[edge.From], edge.To)
			reverse[edge.To] = append(reverse[edge.To], edge.From)
		}
		seen, order := make([]bool, d.Nodes), []int{}
		var finish func(int)
		finish = func(node int) {
			if seen[node] {
				return
			}
			seen[node] = true
			for _, next := range adj[node] {
				finish(next)
			}
			order = append(order, node)
		}
		for node := 0; node < d.Nodes; node++ {
			finish(node)
		}
		groups, assigned := [][]int{}, map[int]int{}
		var collect func(int, int)
		collect = func(node, group int) {
			if _, exists := assigned[node]; exists {
				return
			}
			assigned[node] = group
			groups[group] = append(groups[group], node)
			for _, next := range reverse[node] {
				collect(next, group)
			}
		}
		for i := len(order) - 1; i >= 0; i-- {
			if _, exists := assigned[order[i]]; !exists {
				groups = append(groups, []int{})
				collect(order[i], len(groups)-1)
			}
		}
		in, out := map[int]bool{}, map[int]bool{}
		for _, edge := range d.Edges {
			if assigned[edge.From] != assigned[edge.To] {
				out[assigned[edge.From]], in[assigned[edge.To]] = true, true
			}
		}
		needed := max(len(groups)-len(in), len(groups)-len(out))
		if len(groups) == 1 {
			needed = 0
		}
		for _, group := range groups {
			sort.Ints(group)
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i][0] < groups[j][0] })
		return map[string]any{"components": groups, "edges_needed": needed}
	case "weighted-edit-distance":
		d := codingQuestion[benchmarkEditData](t, item)
		n, m := len(d.Source), len(d.Target)
		distance, visited := make([]int, (n+1)*(m+1)), make([]bool, (n+1)*(m+1))
		for i := 1; i < len(distance); i++ {
			distance[i] = 1 << 25
		}
		for {
			best := -1
			for i := range distance {
				if !visited[i] && (best < 0 || distance[i] < distance[best]) {
					best = i
				}
			}
			if best == len(distance)-1 {
				return distance[best]
			}
			visited[best] = true
			i, j := best/(m+1), best%(m+1)
			relax := func(a, b, cost int) {
				next := a*(m+1) + b
				if distance[best]+cost < distance[next] {
					distance[next] = distance[best] + cost
				}
			}
			if i < n {
				relax(i+1, j, d.Delete)
			}
			if j < m {
				relax(i, j+1, d.Insert)
			}
			if i < n && j < m {
				cost := d.Replace
				if d.Source[i] == d.Target[j] {
					cost = 0
				}
				relax(i+1, j+1, cost)
			}
			if i+1 < n && j+1 < m && d.Source[i] == d.Target[j+1] && d.Source[i+1] == d.Target[j] {
				relax(i+2, j+2, d.Transpose)
			}
		}
	case "lis-count":
		values := codingQuestion[[]int](t, item)
		longest, count := 0, 0
		for mask := 1; mask < 1<<len(values); mask++ {
			previous, length, increasing := -1<<25, 0, true
			for i, value := range values {
				if mask>>i&1 != 0 {
					increasing = increasing && value > previous
					previous = value
					length++
				}
			}
			if increasing {
				if length > longest {
					longest, count = length, 1
				} else if length == longest {
					count++
				}
			}
		}
		return map[string]any{"length": longest, "count": count}
	case "range-update-query":
		d := codingQuestion[benchmarkRangeData](t, item)
		out := []int{}
		for _, op := range d.Operations {
			sum := 0
			for i := op.Left; i <= op.Right; i++ {
				if op.Op == "add" {
					d.Initial[i] += op.Delta
				} else {
					sum += d.Initial[i]
				}
			}
			if op.Op == "sum" {
				out = append(out, sum)
			}
		}
		return out
	case "bitmask-shortest-path":
		d := codingQuestion[benchmarkKeyGraph](t, item)
		const inf = 1 << 25
		distance := make([][8]int, d.Nodes)
		for node := range distance {
			for mask := range distance[node] {
				distance[node][mask] = inf
			}
		}
		distance[0][d.Keys[0]] = 0
		for iteration := 0; iteration < d.Nodes*8; iteration++ {
			changed := false
			for _, edge := range d.Edges {
				for mask := 0; mask < 8; mask++ {
					if distance[edge.From][mask] == inf || mask&edge.Need != edge.Need {
						continue
					}
					next, cost := mask|d.Keys[edge.To], distance[edge.From][mask]+edge.Cost
					if cost < distance[edge.To][next] {
						distance[edge.To][next], changed = cost, true
					}
				}
			}
			if !changed {
				break
			}
		}
		best := inf
		for _, value := range distance[d.Nodes-1] {
			best = min(best, value)
		}
		if best == inf {
			return -1
		}
		return best
	}
	t.Fatalf("missing independent coding oracle for %s", item.FamilyID)
	return nil
}
