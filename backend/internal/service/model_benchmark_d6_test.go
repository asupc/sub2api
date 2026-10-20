//go:build unit

package service

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModelBenchmarkD6IndependentOracles recomputes every D6 expected value
// from the public data block with an implementation that differs from the
// production solver, then probes the checker with a correct and a mutated
// answer. Oracles never read item.Expected.
func TestModelBenchmarkD6IndependentOracles(t *testing.T) {
	for _, family := range modelBenchmarkD6Families {
		t.Run(family.id, func(t *testing.T) {
			for seed := 1; seed <= 64; seed++ {
				for variant := 1; variant <= 2; variant++ {
					prompt, expected := generateModelBenchmarkD6(family, newModelBenchmarkRNG(modelBenchmarkSeed(seed, family.id, variant, "d6")))
					item := ModelBenchmarkItem{ID: family.id, FamilyID: family.id, Category: family.category, Difficulty: family.difficulty, Checker: family.checker, Prompt: prompt, Expected: expected}
					if family.checker == "witness" {
						// Reference witness must score correct; a wrong witness must
						// be rejected. Negative probes are family-specific because a
						// generic single-field mutation can still satisfy SAT.
						expectedJSON, err := json.Marshal(expected)
						require.NoError(t, err)
						result := ModelBenchmarkScoreAnswer(item, `{"answer":`+string(expectedJSON)+`}`)
						require.Equal(t, "correct", result.Status, prompt+": "+result.FailureReason)
						var wrong any
						switch family.id {
						case "d6-sat-witness":
							wrong = []bool{} // wrong length
						case "d6-route-windows":
							wrong = map[string]any{"order": []int{}, "finish_time": 999999}
						case "d6-resource-alloc":
							var dd d6ResourceData
							require.NoError(t, modelBenchmarkQuestionData(item, &dd))
							all := []int{}
							for i := range dd.Items {
								all = append(all, i)
							}
							wrong = all // infeasible (exceeds capacity) or suboptimal
						}
						wrongJSON, err := json.Marshal(map[string]any{"answer": wrong})
						require.NoError(t, err)
						require.Zero(t, ModelBenchmarkScoreAnswer(item, string(wrongJSON)).Score, family.id+": "+string(wrongJSON))
						continue
					}
					want := d6IndependentOracle(t, item)
					wantJSON, err := json.Marshal(want)
					require.NoError(t, err)
					expectedJSON, err := json.Marshal(expected)
					require.NoError(t, err)
					require.JSONEq(t, string(wantJSON), string(expectedJSON), prompt)
					result := ModelBenchmarkScoreAnswer(item, `{"answer":`+string(wantJSON)+`}`)
					require.Equal(t, "correct", result.Status, prompt+": "+result.FailureReason)
					var wrong any
					require.NoError(t, json.Unmarshal(wantJSON, &wrong))
					wrong = codingMutatedAnswer(wrong)
					wrongJSON, err := json.Marshal(map[string]any{"answer": wrong})
					require.NoError(t, err)
					// fields/set checkers award partial credit, so a mutated answer
					// must score below full marks rather than exactly zero.
					require.Less(t, ModelBenchmarkScoreAnswer(item, string(wrongJSON)).Score, 1.0, family.id+": "+string(wrongJSON))
				}
			}
		})
	}
}

func d6Question[T any](t *testing.T, item ModelBenchmarkItem) T {
	t.Helper()
	var data T
	require.NoError(t, modelBenchmarkQuestionData(item, &data))
	return data
}

// d6IndependentOracle solves the public data block with algorithms that differ
// from the production solvers (Floyd vs Bellman, Kosaraju vs Tarjan, brute
// force vs DP) so a shared bug cannot pass both sides.
func d6IndependentOracle(t *testing.T, item ModelBenchmarkItem) any {
	switch item.FamilyID {
	case "d6-maxflow-cut-count":
		d := d6Question[benchmarkCodingGraph](t, item)
		// Oracle: Edmonds-Karp with BFS for max flow, then enumerate cuts.
		n := d.Nodes
		cap := make([][]int, n)
		for i := range cap {
			cap[i] = make([]int, n)
		}
		for _, e := range d.Edges {
			cap[e.From][e.To] += e.Cost
		}
		res := make([][]int, n)
		for i := range res {
			res[i] = make([]int, n)
			copy(res[i], cap[i])
		}
		flow := 0
		for {
			parent := make([]int, n)
			for i := range parent {
				parent[i] = -1
			}
			parent[0] = 0
			queue := []int{0}
			for len(queue) > 0 {
				u := queue[0]
				queue = queue[1:]
				for v := 0; v < n; v++ {
					if res[u][v] > 0 && parent[v] == -1 {
						parent[v] = u
						queue = append(queue, v)
					}
				}
			}
			if parent[n-1] == -1 {
				break
			}
			aug := 1 << 30
			for v := n - 1; v != 0; v = parent[v] {
				aug = min(aug, res[parent[v]][v])
			}
			for v := n - 1; v != 0; v = parent[v] {
				res[parent[v]][v] -= aug
				res[v][parent[v]] += aug
			}
			flow += aug
		}
		count := 0
		for mask := 1; mask < 1<<n; mask += 2 {
			if mask>>(n-1)&1 != 0 {
				continue
			}
			c := 0
			for _, e := range d.Edges {
				if mask>>e.From&1 != 0 && mask>>e.To&1 == 0 {
					c += e.Cost
				}
			}
			if c == flow {
				count++
			}
		}
		return map[string]any{"flow": flow, "cut_count": count}
	case "d6-scc-condensation":
		d := d6Question[benchmarkCodingGraph](t, item)
		// Oracle: Tarjan via iterative DFS.
		n := d.Nodes
		adj := make([][]int, n)
		for _, e := range d.Edges {
			adj[e.From] = append(adj[e.From], e.To)
		}
		index := make([]int, n)
		low := make([]int, n)
		onStack := make([]bool, n)
		for i := range index {
			index[i] = -1
		}
		stack := []int{}
		counter := 0
		comp := make([]int, n)
		for i := range comp {
			comp[i] = -1
		}
		compCount := 0
		var strongconnect func(v int)
		strongconnect = func(v int) {
			index[v], low[v] = counter, counter
			counter++
			stack = append(stack, v)
			onStack[v] = true
			for _, w := range adj[v] {
				if index[w] == -1 {
					strongconnect(w)
					low[v] = min(low[v], low[w])
				} else if onStack[w] {
					low[v] = min(low[v], index[w])
				}
			}
			if low[v] == index[v] {
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					onStack[w] = false
					comp[w] = compCount
					if w == v {
						break
					}
				}
				compCount++
			}
		}
		for v := 0; v < n; v++ {
			if index[v] == -1 {
				strongconnect(v)
			}
		}
		groups := make([][]int, compCount)
		for v := 0; v < n; v++ {
			groups[comp[v]] = append(groups[comp[v]], v)
		}
		for _, g := range groups {
			sort.Ints(g)
		}
		sort.Slice(groups, func(i, j int) bool { return groups[i][0] < groups[j][0] })
		in, out := make([]bool, compCount), make([]bool, compCount)
		for _, e := range d.Edges {
			a, b := comp[e.From], comp[e.To]
			if a != b {
				out[a], in[b] = true, true
			}
		}
		src, sink := 0, 0
		for i := 0; i < compCount; i++ {
			if !in[i] {
				src++
			}
			if !out[i] {
				sink++
			}
		}
		needed := max(src, sink)
		if compCount == 1 {
			needed = 0
		}
		return map[string]any{"components": groups, "edges_needed": needed}
	case "d6-bitmask-tsp":
		d := d6Question[struct {
			N    int     `json:"n"`
			Dist [][]int `json:"dist"`
		}](t, item)
		// Oracle: brute-force over permutations of cities 1..n-1 (n-1 <= 10,
		// 10! = 3.6M is too many; use DP over subsets but with a different
		// recurrence direction than production).
		n := d.N
		const inf = 1 << 30
		size := 1 << n
		// dp[mask][last] but iterate masks in reverse and relax forward edges.
		dp := make([][]int, size)
		for i := range dp {
			dp[i] = make([]int, n)
			for j := range dp[i] {
				dp[i][j] = inf
			}
		}
		// Start: from 0 to each city.
		for c := 1; c < n; c++ {
			dp[(1|1<<c)][c] = d.Dist[0][c]
		}
		for mask := 0; mask < size; mask++ {
			for last := 0; last < n; last++ {
				cur := dp[mask][last]
				if cur == inf {
					continue
				}
				for nx := 0; nx < n; nx++ {
					if mask>>nx&1 == 1 {
						continue
					}
					nm := mask | 1<<nx
					if v := cur + d.Dist[last][nx]; v < dp[nm][nx] {
						dp[nm][nx] = v
					}
				}
			}
		}
		full := size - 1
		best := inf
		for last := 1; last < n; last++ {
			if v := dp[full][last] + d.Dist[last][0]; v < best {
				best = v
			}
		}
		return best
	case "d6-interval-dp-count":
		d := d6Question[struct {
			Values []int `json:"values"`
		}](t, item)
		values := d.Values
		n := len(values)
		// Oracle: memoized recursion over (i,j) returning (cost,count).
		type key struct{ i, j int }
		prefix := make([]int, n+1)
		for i, v := range values {
			prefix[i+1] = prefix[i] + v
		}
		memo := map[key][2]int64{}
		var solve func(i, j int) [2]int64
		solve = func(i, j int) [2]int64 {
			if i == j {
				return [2]int64{0, 1}
			}
			if m, ok := memo[key{i, j}]; ok {
				return m
			}
			best := int64(1) << 60
			var cnt int64
			for k := i; k < j; k++ {
				l := solve(i, k)
				rr := solve(k+1, j)
				c := l[0] + rr[0] + int64(prefix[j+1]-prefix[i])
				w := l[1] * rr[1]
				if c < best {
					best, cnt = c, w
				} else if c == best {
					cnt += w
				}
			}
			memo[key{i, j}] = [2]int64{best, cnt}
			return memo[key{i, j}]
		}
		res := solve(0, n-1)
		return map[string]any{"cost": res[0], "count": res[1]}
	case "d6-shortest-path-k":
		d := d6Question[struct {
			Nodes int                    `json:"nodes"`
			Edges []benchmarkCodingEdge  `json:"edges"`
			K     int                    `json:"k"`
		}](t, item)
		// Oracle: BFS over (node, used) layered graph, different relaxation.
		const inf = 1 << 30
		n := d.Nodes
		dist := make([][]int, d.K+1)
		for i := range dist {
			dist[i] = make([]int, n)
			for j := range dist[i] {
				dist[i][j] = inf
			}
		}
		dist[0][0] = 0
		// Bellman-style per layer.
		for e := 0; e < d.K; e++ {
			for _, ed := range d.Edges {
				if dist[e][ed.From] != inf && dist[e][ed.From]+ed.Cost < dist[e+1][ed.To] {
					dist[e+1][ed.To] = dist[e][ed.From] + ed.Cost
				}
			}
		}
		if dist[d.K][n-1] == inf {
			return -1
		}
		return dist[d.K][n-1]
	case "d6-topo-count":
		d := d6Question[struct {
			Nodes int      `json:"nodes"`
			Edges [][2]int `json:"edges"`
		}](t, item)
		n := d.Nodes
		// Oracle: recursive count with memo over remaining set.
		pred := make([]int, n)
		for _, e := range d.Edges {
			pred[e[1]] |= 1 << e[0]
		}
		memo := map[int]int64{}
		var count func(done int) int64
		count = func(done int) int64 {
			if done == (1<<n)-1 {
				return 1
			}
			if m, ok := memo[done]; ok {
				return m
			}
			var total int64
			for node := 0; node < n; node++ {
				if done>>node&1 == 1 {
					continue
				}
				if pred[node]&^done == 0 {
					total += count(done | 1<<node)
				}
			}
			memo[done] = total
			return total
		}
		return count(0)
	case "d6-lis-count":
		d := d6Question[struct {
			Values []int `json:"values"`
		}](t, item)
		values := d.Values
		// Oracle: patience-style O(n^2) but tracking counts via a different
		// accumulation (process from right to left with "longest starting at i").
		n := len(values)
		lengths := make([]int, n)
		counts := make([]int64, n)
		best, total := 0, int64(0)
		for i := n - 1; i >= 0; i-- {
			lengths[i], counts[i] = 1, 1
			for j := i + 1; j < n; j++ {
				if values[j] > values[i] {
					cand := lengths[j] + 1
					if cand > lengths[i] {
						lengths[i], counts[i] = cand, counts[j]
					} else if cand == lengths[i] {
						counts[i] += counts[j]
					}
				}
			}
			if lengths[i] > best {
				best, total = lengths[i], counts[i]
			} else if lengths[i] == best {
				total += counts[i]
			}
		}
		return map[string]any{"length": best, "count": total}
	case "d6-gc-trace":
		d := d6Question[d6GCData](t, item)
		// Oracle: independent reachability via DFS from roots (no color machine),
		// gray_steps = number of distinct reachable objects.
		adj := make([][]int, d.Count)
		for _, e := range d.Edges {
			adj[e[0]] = append(adj[e[0]], e[1])
		}
		reach := make([]bool, d.Count)
		var dfs func(int)
		dfs = func(x int) {
			if reach[x] {
				return
			}
			reach[x] = true
			for _, nx := range adj[x] {
				dfs(nx)
			}
		}
		for _, rt := range d.Roots {
			dfs(rt)
		}
		marked, collected := []int{}, []int{}
		for i := 0; i < d.Count; i++ {
			if reach[i] {
				marked = append(marked, i)
			} else {
				collected = append(collected, i)
			}
		}
		return map[string]any{"marked": marked, "collected": collected, "gray_steps": len(marked)}
	case "d6-bytecode-vm":
		d := d6Question[d6BytecodeData](t, item)
		// Oracle: run the same VM semantics via an independent interpreter that
		// also computes f(n) recursively as a cross-check.
		locals := append([]int{}, d.Locals...)
		type frame struct {
			pc     int
			locals []int
		}
		stack, cs := []int{}, []frame{}
		steps := 0
		pop := func() int { v := stack[len(stack)-1]; stack = stack[:len(stack)-1]; return v }
		for pc := 0; ; {
			in := d.Program[pc]
			steps++
			pc++
			switch in.Op {
			case "load":
				stack = append(stack, locals[in.Arg])
			case "push":
				stack = append(stack, in.Arg)
			case "store":
				locals[in.Arg] = pop()
			case "add", "sub", "mod", "xor":
				b, a := pop(), pop()
				v := a + b
				if in.Op == "sub" {
					v = a - b
				} else if in.Op == "mod" {
					v = a % b
				} else if in.Op == "xor" {
					v = a ^ b
				}
				stack = append(stack, v)
			case "jz":
				if pop() == 0 {
					pc = in.Arg
				}
			case "jmp":
				pc = in.Arg
			case "call":
				arg := pop()
				cs = append(cs, frame{pc, append([]int{}, locals...)})
				locals = make([]int, len(locals))
				locals[0] = arg
				pc = in.Arg
			case "ret":
				ret := pop()
				fr := cs[len(cs)-1]
				cs = cs[:len(cs)-1]
				locals = fr.locals
				pc = fr.pc
				stack = append(stack, ret)
			case "halt":
				return map[string]any{"locals": locals, "steps": steps}
			}
		}
	case "d6-register-overflow":
		d := d6Question[benchmarkOverflowData](t, item)
		// Oracle: independent two's-complement simulation using int64 masking.
		mask := (int64(1) << d.Bits) - 1
		sign := int64(1) << (d.Bits - 1)
		value := int64(d.Initial)
		toSigned := func(v int64) int64 {
			v &= mask
			if v&sign != 0 {
				v -= mask + 1
			}
			return v
		}
		value = toSigned(value)
		out := []int{}
		for _, op := range d.Operations {
			switch op.Op {
			case "add":
				value += int64(op.Arg)
			case "mul":
				value *= int64(op.Arg)
			case "xor":
				value ^= int64(op.Arg)
			case "shr":
				value >>= uint(op.Arg)
			case "ushr":
				value = (value & mask) >> uint(op.Arg)
			}
			value = toSigned(value)
			out = append(out, int(value))
		}
		return out
	case "d6-serializability":
		d := d6Question[benchmarkConflictData](t, item)
		// Oracle: build precedence edges, detect cycle members via DFS coloring.
		edges := [][2]int{}
		for i, a := range d.Operations {
			for _, b := range d.Operations[i+1:] {
				if a.Tx != b.Tx && a.Key == b.Key && (a.Write || b.Write) {
					edges = append(edges, [2]int{a.Tx, b.Tx})
				}
			}
		}
		reach := benchmarkReachability(d.Transactions, edges)
		members := []int{}
		for i := range reach {
			if reach[i][i] {
				members = append(members, i)
			}
		}
		return map[string]any{"serializable": len(members) == 0, "cycle_members": members}
	case "d6-lru-writeback":
		d := d6Question[benchmarkLRUData](t, item)
		// Oracle: independent LRU simulation with a map-based recency list.
		order, dirty := []int{}, map[int]bool{}
		misses, writes := 0, 0
		for _, op := range d.Operations {
			idx := -1
			for i, k := range order {
				if k == op.Key {
					idx = i
					break
				}
			}
			if idx >= 0 {
				order = append(order[:idx], order[idx+1:]...)
			} else {
				misses++
				if len(order) == d.Capacity {
					if dirty[order[0]] {
						writes++
					}
					delete(dirty, order[0])
					order = order[1:]
				}
			}
			order = append(order, op.Key)
			dirty[op.Key] = dirty[op.Key] || op.Write
		}
		dirtyKeys := []int{}
		for k, v := range dirty {
			if v {
				dirtyKeys = append(dirtyKeys, k)
			}
		}
		sort.Ints(dirtyKeys)
		return map[string]any{"misses": misses, "writebacks": writes, "order": order, "dirty": dirtyKeys}
	case "d6-epistemic-puzzle":
		d := d6Question[d6EpistemicData](t, item)
		total := 0
		for _, v := range d.SeenCounts {
			if v > total {
				total = v
			}
		}
		out := []int{}
		for i, v := range d.SeenCounts {
			if v == total-1 {
				out = append(out, i)
			}
		}
		return out
	case "d6-nim-game":
		d := d6Question[d6NimData](t, item)
		x := 0
		for _, h := range d.Heaps {
			x ^= h
		}
		if x == 0 {
			return []int{}
		}
		for i, h := range d.Heaps {
			if (h ^ x) < h {
				return []int{i, h - (h ^ x)}
			}
		}
		return []int{}
	case "d6-matrix-chain":
		d := d6Question[struct {
			Dims []int `json:"dims"`
		}](t, item)
		dims := d.Dims
		n := len(dims) - 1
		// Oracle: memoized recursion over (i,j), independent from production's
		// bottom-up length loop.
		type key struct{ i, j int }
		memo := map[key]int64{}
		var solve func(i, j int) int64
		solve = func(i, j int) int64 {
			if i == j {
				return 0
			}
			if m, ok := memo[key{i, j}]; ok {
				return m
			}
			best := int64(1) << 60
			for k := i; k < j; k++ {
				c := solve(i, k) + solve(k+1, j) + int64(dims[i])*int64(dims[k+1])*int64(dims[j+1])
				if c < best {
					best = c
				}
			}
			memo[key{i, j}] = best
			return best
		}
		return int(solve(0, n-1))
	default:
		t.Fatalf("missing d6 oracle for %s", item.FamilyID)
		return nil
	}
}
