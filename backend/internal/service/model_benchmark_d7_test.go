//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModelBenchmarkD7IndependentOracles recomputes every D7 expected value
// from the public data block with algorithms that differ from production
// solvers (table filling vs partition refinement, enumeration vs Ryser,
// Dijkstra-style closure vs naive relaxation) and probes each checker with a
// mutated wrong answer. Oracles never read item.Expected.
func TestModelBenchmarkD7IndependentOracles(t *testing.T) {
	for _, family := range modelBenchmarkD7Families {
		t.Run(family.id, func(t *testing.T) {
			for seed := 1; seed <= 64; seed++ {
				for variant := 1; variant <= 2; variant++ {
					prompt, expected := generateModelBenchmarkD7(family, newModelBenchmarkRNG(modelBenchmarkSeed(seed, family.id, variant, "d7")))
					item := ModelBenchmarkItem{ID: family.id, FamilyID: family.id, Category: family.category, Difficulty: family.difficulty, Checker: family.checker, Prompt: prompt, Expected: expected}
					want := d7IndependentOracle(t, item)
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
					require.Less(t, ModelBenchmarkScoreAnswer(item, string(wrongJSON)).Score, 1.0, family.id+": "+string(wrongJSON))
				}
			}
		})
	}
}

func d7Question[T any](t *testing.T, item ModelBenchmarkItem) T {
	t.Helper()
	var data T
	require.NoError(t, modelBenchmarkQuestionData(item, &data))
	return data
}

func d7IndependentOracle(t *testing.T, item ModelBenchmarkItem) any {
	t.Helper()
	switch item.FamilyID {
	case "d7-dfa-minimize":
		d := d7Question[d7DFAData](t, item)
		// Oracle: Hopcroft-style worklist refinement on reachable states.
		reachable := []int{}
		seen := make([]bool, d.States)
		stack := []int{d.Start}
		seen[d.Start] = true
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			reachable = append(reachable, node)
			for _, next := range d.Transitions[node] {
				if !seen[next] {
					seen[next] = true
					stack = append(stack, next)
				}
			}
		}
		accepting := map[int]bool{}
		for _, state := range d.Accepting {
			accepting[state] = true
		}
		partition := [][]int{}
		acceptBlock, rejectBlock := []int{}, []int{}
		for _, state := range reachable {
			if accepting[state] {
				acceptBlock = append(acceptBlock, state)
			} else {
				rejectBlock = append(rejectBlock, state)
			}
		}
		if len(acceptBlock) > 0 {
			partition = append(partition, acceptBlock)
		}
		if len(rejectBlock) > 0 {
			partition = append(partition, rejectBlock)
		}
		var blockOf func(int) int
		blockOf = func(state int) int {
			for index, block := range partition {
				for _, member := range block {
					if member == state {
						return index
					}
				}
			}
			return -1
		}
		for {
			changed := false
			next := [][]int{}
			for _, block := range partition {
				groups := map[[2]int][]int{}
				order := [][2]int{}
				for _, state := range block {
					key := [2]int{blockOf(d.Transitions[state][0]), blockOf(d.Transitions[state][1])}
					if _, exists := groups[key]; !exists {
						order = append(order, key)
					}
					groups[key] = append(groups[key], state)
				}
				if len(groups) > 1 {
					changed = true
				}
				for _, key := range order {
					next = append(next, groups[key])
				}
			}
			partition = next
			if !changed {
				break
			}
		}
		// 最短字符串长度用逐状态 BFS 求：按长度分层的显式字符串枚举在
		// 18 状态随机 DFA 上会指数爆炸，这里改走状态图 + map 队列。
		shortest := func(wantAccept bool) int {
			type entry struct {
				state  int
				length int
			}
			queue := []entry{{d.Start, 0}}
			visited := map[int]bool{d.Start: true}
			for len(queue) > 0 {
				current := queue[0]
				queue = queue[1:]
				if accepting[current.state] == wantAccept {
					return current.length
				}
				for symbol := 0; symbol < 2; symbol++ {
					next := d.Transitions[current.state][symbol]
					if !visited[next] {
						visited[next] = true
						queue = append(queue, entry{next, current.length + 1})
					}
				}
			}
			return -1
		}
		return map[string]any{
			"minimal_states":    len(partition),
			"shortest_accepted": shortest(true),
			"shortest_rejected": shortest(false),
		}
	case "d7-regex-complement":
		d := d7Question[d7RegexQuestion](t, item)
		// Oracle: recursive span matcher over the AST (no NFA construction),
		// then level-order enumeration of candidate strings.
		var matchSpan func(node d7RegexNode, text string, from, to int) bool
		matchSpan = func(node d7RegexNode, text string, from, to int) bool {
			switch node.Op {
			case "lit":
				return to-from == 1 && text[from:to] == node.Lit
			case "cat":
				positions := make([]bool, len(text)+1)
				positions[from] = true
				for _, child := range node.Children {
					next := make([]bool, len(text)+1)
					for offset := from; offset <= len(text); offset++ {
						if !positions[offset] {
							continue
						}
						for end := offset; end <= len(text); end++ {
							if matchSpan(child, text, offset, end) {
								next[end] = true
							}
						}
					}
					positions = next
				}
				return positions[to]
			case "alt":
				for _, child := range node.Children {
					if matchSpan(child, text, from, to) {
						return true
					}
				}
				return false
			case "star":
				seen := make([]bool, len(text)+1)
				seen[from] = true
				queue := []int{from}
				for len(queue) > 0 {
					offset := queue[0]
					queue = queue[1:]
					if offset == to {
						return true
					}
					for end := offset + 1; end <= len(text); end++ {
						if !seen[end] && matchSpan(*node.Child, text, offset, end) {
							seen[end] = true
							queue = append(queue, end)
						}
					}
				}
				return seen[to]
			}
			return false
		}
		match := func(text string) bool { return matchSpan(d.Regex, text, 0, len(text)) }
		if !match("") {
			return 0
		}
		level := []string{""}
		for length := 1; length <= 14; length++ {
			next := []string{}
			for _, prefix := range level {
				for _, symbol := range []string{"a", "b"} {
					candidate := prefix + symbol
					if !match(candidate) {
						return length
					}
					next = append(next, candidate)
				}
			}
			level = next
		}
		t.Fatalf("regex matches all strings up to length 14: %+v", d.Regex)
		return nil
	case "d7-permanent-mod":
		d := d7Question[d7PermanentData](t, item)
		// Oracle: brute-force permutation enumeration for the smaller n, and
		// DP over column subsets for larger n (different from Ryser).
		n := d.N
		const mod = int64(1000000007)
		dp := make([]int64, 1<<n)
		dp[0] = 1
		for mask := 0; mask < 1<<n; mask++ {
			row := 0
			for value := mask; value != 0; value &= value - 1 {
				row++
			}
			if row == n {
				continue
			}
			for col := 0; col < n; col++ {
				if mask>>col&1 == 0 {
					dp[mask|1<<col] = (dp[mask|1<<col] + dp[mask]*int64(d.Matrix[row][col])) % mod
				}
			}
		}
		return dp[(1<<n)-1]
	case "d7-vector-clock":
		d := d7Question[d7VectorData](t, item)
		// Oracle: per-process event logs merged by message pairs, computed by
		// repeatedly applying "local tick then max with received message".
		out := []int{}
		clocks := make([][]int, d.Processes)
		for i := range clocks {
			clocks[i] = make([]int, d.Processes)
		}
		messages := map[int][]int{}
		for index, event := range d.Events {
			p := event.Process
			// tick
			clocks[p][p]++
			if event.Kind == "recv" {
				source, ok := messages[event.SendIndex]
				require.True(t, ok, "recv must reference an earlier send")
				for i := 0; i < d.Processes; i++ {
					if source[i] > clocks[p][i] {
						clocks[p][i] = source[i]
					}
				}
			}
			if event.Kind == "send" {
				snapshot := make([]int, d.Processes)
				copy(snapshot, clocks[p])
				messages[index] = snapshot
			}
			out = append(out, clocks[p]...)
		}
		return out
	case "d7-crdt-merge":
		d := d7Question[d7CRDTData](t, item)
		// Oracle: collect all registers per key and pick max by explicit sort.
		all := map[string][]d7Register{}
		for _, replica := range d.Replicas {
			for key, register := range replica {
				all[key] = append(all[key], register)
			}
		}
		result := map[string]any{}
		for key, registers := range all {
			sort.Slice(registers, func(i, j int) bool {
				if registers[i].Counter != registers[j].Counter {
					return registers[i].Counter < registers[j].Counter
				}
				return registers[i].Node < registers[j].Node
			})
			result[key] = registers[len(registers)-1].Value
		}
		return result
	case "d7-list-scheduling":
		d := d7Question[d7ScheduleData](t, item)
		// Oracle: discrete-event simulation with a priority queue of ready jobs
		// and a per-machine finish time heap.
		require.Equal(t, 3, d.Machines)
		n := len(d.Jobs)
		finish := make([]int, n)
		started := make([]bool, n)
		machines := make([]int, d.Machines)
		time := 0
		for done := 0; done < n; {
			ready := []int{}
			for i := 0; i < n; i++ {
				if started[i] {
					continue
				}
				blocked := false
				for _, parent := range d.Jobs[i].Parents {
					if !started[parent] || finish[parent] > time {
						blocked = true
						break
					}
				}
				if !blocked {
					ready = append(ready, i)
				}
			}
			sort.Ints(ready)
			cursor := 0
			for m := 0; m < d.Machines && cursor < len(ready); m++ {
				if machines[m] > time {
					continue
				}
				job := ready[cursor]
				cursor++
				finish[job] = time + d.Jobs[job].Duration
				machines[m] = finish[job]
				started[job] = true
				done++
			}
			if done == n {
				break
			}
			next := -1
			for _, value := range machines {
				if value > time && (next == -1 || value < next) {
					next = value
				}
			}
			require.NotEqual(t, -1, next)
			time = next
		}
		makespan := 0
		for _, value := range finish {
			makespan = max(makespan, value)
		}
		return makespan
	case "d7-datalog-fixpoint":
		d := d7Question[d7DatalogData](t, item)
		// Oracle: semi-naive closure that applies the three rules in a single
		// loop over adjacency lists, independent of production's map joins.
		adjR := make([][]int, d.Nodes)
		adjS := make([][]int, d.Nodes)
		adjT := make([][]int, d.Nodes)
		hasR := make([][]bool, d.Nodes)
		hasS := make([][]bool, d.Nodes)
		hasT := make([][]bool, d.Nodes)
		for i := 0; i < d.Nodes; i++ {
			hasR[i] = make([]bool, d.Nodes)
			hasS[i] = make([]bool, d.Nodes)
			hasT[i] = make([]bool, d.Nodes)
		}
		add := func(from, to int, relation [][]bool, adjacency *[][]int) bool {
			if relation[from][to] {
				return false
			}
			relation[from][to] = true
			(*adjacency)[from] = append((*adjacency)[from], to)
			return true
		}
		for _, pair := range d.R {
			if pair[0] < d.Nodes && pair[1] < d.Nodes {
				add(pair[0], pair[1], hasR, &adjR)
			}
		}
		for _, pair := range d.S {
			if pair[0] < d.Nodes && pair[1] < d.Nodes {
				add(pair[0], pair[1], hasS, &adjS)
			}
		}
		for {
			added := false
			for x := 0; x < d.Nodes; x++ {
				for _, y := range adjR[x] {
					for _, z := range adjS[y] {
						if add(x, z, hasT, &adjT) {
							added = true
						}
					}
				}
			}
			for x := 0; x < d.Nodes; x++ {
				for _, y := range adjS[x] {
					for _, z := range adjT[y] {
						if add(x, z, hasR, &adjR) {
							added = true
						}
					}
				}
			}
			for x := 0; x < d.Nodes; x++ {
				for _, y := range adjR[x] {
					for _, z := range adjR[y] {
						if add(x, z, hasS, &adjS) {
							added = true
						}
					}
				}
			}
			if !added {
				break
			}
		}
		tSet := map[[2]int]bool{}
		for x := 0; x < d.Nodes; x++ {
			for _, y := range adjT[x] {
				tSet[[2]int{x, y}] = true
			}
		}
		out := []string{}
		for pair := range tSet {
			out = append(out, fmt.Sprintf("%d,%d", pair[0], pair[1]))
		}
		sort.Slice(out, func(i, j int) bool {
			left, right := out[i], out[j]
			if left == right {
				return false
			}
			var lx, ly, rx, ry int
			fmt.Sscanf(left, "%d,%d", &lx, &ly)
			fmt.Sscanf(right, "%d,%d", &rx, &ry)
			if lx != rx {
				return lx < rx
			}
			return ly < ry
		})
		return out
	case "d7-planning-min-steps":
		d := d7Question[d7PlanningData](t, item)
		// Oracle: Dijkstra-style priority expansion (unit weights) over the
		// same state graph used by production BFS, via an explicit heap.
		maskOf := func(values []int) int {
			mask := 0
			for _, value := range values {
				mask |= 1 << value
			}
			return mask
		}
		initial, goal := maskOf(d.Initial), maskOf(d.Goal)
		type item struct{ cost, state int }
		heap := []item{{0, initial}}
		best := map[int]int{initial: 0}
		for len(heap) > 0 {
			sort.Slice(heap, func(i, j int) bool { return heap[i].cost < heap[j].cost })
			current := heap[0]
			heap = heap[1:]
			if current.cost != best[current.state] {
				continue
			}
			if current.state&goal == goal {
				return current.cost
			}
			for _, action := range d.Actions {
				pre := maskOf(action.Pre)
				if current.state&pre != pre {
					continue
				}
				next := (current.state &^ maskOf(action.Del)) | maskOf(action.Add)
				if old, seen := best[next]; !seen || current.cost+1 < old {
					best[next] = current.cost + 1
					heap = append(heap, item{current.cost + 1, next})
				}
			}
		}
		return -1
	default:
		t.Fatalf("missing d7 oracle for %s", item.FamilyID)
		return nil
	}
}
