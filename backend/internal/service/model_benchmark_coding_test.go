//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func codingQuestion[T any](t *testing.T, item ModelBenchmarkItem) T {
	t.Helper()
	var data T
	require.NoError(t, modelBenchmarkQuestionData(item, &data))
	return data
}

func TestModelBenchmarkCodingIndependentOracles(t *testing.T) {
	for _, family := range modelBenchmarkCodingFamilies {
		t.Run(family.id, func(t *testing.T) {
			for seed := 1; seed <= 128; seed++ {
				for variant := 1; variant <= 2; variant++ {
					prompt, expected := generateModelBenchmarkCoding(family, newModelBenchmarkRNG(modelBenchmarkSeed(seed, family.id, variant)), variant)
					item := ModelBenchmarkItem{ID: family.id, FamilyID: family.id, Category: family.category, Difficulty: family.difficulty, Checker: family.checker, Prompt: prompt, Expected: expected}
					want := codingIndependentOracle(t, item)
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
					require.Zero(t, ModelBenchmarkScoreAnswer(item, string(wrongJSON)).Score, family.id+": "+string(wrongJSON))
				}
			}
		})
	}
}

func codingMutatedAnswer(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		v[keys[0]] = codingMutatedAnswer(v[keys[0]])
		return v
	case []any:
		if len(v) == 0 {
			return []any{999}
		}
		v[0] = codingMutatedAnswer(v[0])
		return v
	case float64:
		return v + 1000
	case string:
		return v + "_wrong"
	case bool:
		return !v
	}
	return "invalid"
}

// Oracles below never inspect Expected or call a production solver/checker.
func codingIndependentOracle(t *testing.T, item ModelBenchmarkItem) any {
	t.Helper()
	switch item.FamilyID {
	case "slice-alias-trace":
		d := codingQuestion[map[string]any](t, item)
		heap := [][]int{bxInts(d["values"])}
		type view struct{ block, start, length, capacity int }
		base := view{0, 0, len(bxInts(d["values"])), len(bxInts(d["values"]))}
		a := view{0, 1, 3, bxInt(d["limit"]) - 1}
		b := a
		b.length = 2
		appendTo := func(v view, values ...int) view {
			if v.length+len(values) > v.capacity {
				block := make([]int, v.length+len(values))
				copy(block, heap[v.block][v.start:v.start+v.length])
				heap = append(heap, block)
				v.block, v.start, v.capacity = len(heap)-1, 0, len(block)
			}
			copy(heap[v.block][v.start+v.length:], values)
			v.length += len(values)
			return v
		}
		c := appendTo(a, bxInt(d["x"]), bxInt(d["y"]))
		heap[b.block][b.start+1] += bxInt(d["delta"])
		heap[c.block][c.start] -= bxInt(d["sub"])
		b = appendTo(b, bxInt(d["z"]))
		out := map[string]any{}
		for key, v := range map[string]view{"base": base, "a": a, "b": b, "c": c} {
			out[key] = append([]int{}, heap[v.block][v.start:v.start+v.length]...)
		}
		return out
	case "defer-unwind-trace":
		d := codingQuestion[map[string]int](t, item)
		first := (d["a"]*d["b"] + d["e"]) * d["d"]
		second := first + d["a"]
		third := second + d["c"]
		return map[string]any{"result": third, "log": []int{first, second, third}}
	case "event-loop-trace":
		d := codingQuestion[benchmarkEventData](t, item)
		log := []string{}
		flushLayers := func(roots []benchmarkEventTask) {
			for len(roots) > 0 {
				next := []benchmarkEventTask{}
				for _, task := range roots {
					log = append(log, task.Label)
					next = append(next, task.Enqueue...)
				}
				roots = next
			}
		}
		flushLayers(d.Sync)
		for _, timer := range d.Timers {
			flushLayers([]benchmarkEventTask{timer})
		}
		return log
	case "snapshot-write-skew":
		d := codingQuestion[benchmarkSnapshotData](t, item)
		writes, winners := make([]bool, len(d.Targets)), map[int]int{}
		for tx, target := range d.Targets {
			other := 0
			for index, value := range d.Initial {
				if index != target {
					other += value
				}
			}
			writes[tx] = other >= d.Minimum
		}
		committed, balances := []int{}, append([]int{}, d.Initial...)
		for _, tx := range d.Order {
			if !writes[tx] {
				committed = append(committed, tx)
			} else if _, exists := winners[d.Targets[tx]]; !exists {
				winners[d.Targets[tx]] = tx
				committed = append(committed, tx)
			}
		}
		for target := range winners {
			balances[target] = 0
		}
		sum := 0
		for _, value := range balances {
			sum += value
		}
		return map[string]any{"committed": committed, "balances": balances, "invariant": sum >= d.Minimum}
	case "serializability-graph":
		d := codingQuestion[benchmarkConflictData](t, item)
		adj := make([][]int, d.Transactions)
		for later, b := range d.Operations {
			for _, a := range d.Operations[:later] {
				if a.Tx != b.Tx && a.Key == b.Key && (a.Write || b.Write) {
					adj[a.Tx] = append(adj[a.Tx], b.Tx)
				}
			}
		}
		members := []int{}
		for start := range adj {
			seen := map[int]bool{}
			var reachesStart func(int) bool
			reachesStart = func(node int) bool {
				if seen[node] {
					return false
				}
				seen[node] = true
				for _, next := range adj[node] {
					if next == start || reachesStart(next) {
						return true
					}
				}
				return false
			}
			if reachesStart(start) {
				members = append(members, start)
			}
		}
		return map[string]any{"serializable": len(members) == 0, "cycle_members": members}
	case "cas-interleavings":
		d := codingQuestion[map[string]int](t, item)
		type thread struct{ remaining, observed, phase int }
		type state struct {
			threads          []thread
			shared, failures int
		}
		initial := state{threads: make([]thread, d["threads"]), shared: d["initial"]}
		for i := range initial.threads {
			initial.threads[i].remaining = d["increments"]
		}
		queue, histogram := []state{initial}, map[int]int{}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			finished := true
			for i, th := range current.threads {
				if th.remaining == 0 {
					continue
				}
				finished = false
				next := state{threads: append([]thread{}, current.threads...), shared: current.shared, failures: current.failures}
				if th.phase == 0 {
					next.threads[i].observed, next.threads[i].phase = current.shared, 1
				} else {
					next.threads[i].phase = 0
					if th.observed == current.shared {
						next.shared++
						next.threads[i].remaining--
					} else {
						next.failures++
					}
				}
				queue = append(queue, next)
			}
			if finished {
				require.Equal(t, d["initial"]+d["threads"]*d["increments"], current.shared)
				histogram[current.failures]++
			}
		}
		keys := []int{}
		for failures := range histogram {
			keys = append(keys, failures)
		}
		sort.Ints(keys)
		rows := [][]int{}
		for _, failures := range keys {
			rows = append(rows, []int{failures, histogram[failures]})
		}
		return map[string]any{"final": d["initial"] + d["threads"]*d["increments"], "distribution": rows}
	case "lock-cycle-members":
		d := codingQuestion[benchmarkLockData](t, item)
		cycles, blocked := map[int]bool{}, []int{}
		for start := range d.Waiting {
			path, position := []int{}, map[int]int{}
			for node := start; d.Waiting[node] >= 0; node = d.Owner[d.Waiting[node]] {
				if index, exists := position[node]; exists {
					for _, member := range path[index:] {
						cycles[member] = true
					}
					blocked = append(blocked, start)
					break
				}
				position[node] = len(path)
				path = append(path, node)
			}
		}
		members := []int{}
		for member := range cycles {
			members = append(members, member)
		}
		sort.Ints(members)
		return map[string]any{"cycle_members": members, "blocked": blocked}
	case "lru-writeback":
		d := codingQuestion[benchmarkLRUData](t, item)
		type page struct {
			age   int
			dirty bool
		}
		pages, misses, writes := map[int]page{}, 0, 0
		for clock, op := range d.Operations {
			old, exists := pages[op.Key]
			if !exists {
				misses++
				if len(pages) == d.Capacity {
					victim, oldest := -1, len(d.Operations)+1
					for key, p := range pages {
						if p.age < oldest {
							victim, oldest = key, p.age
						}
					}
					if pages[victim].dirty {
						writes++
					}
					delete(pages, victim)
				}
			}
			pages[op.Key] = page{clock, old.dirty || op.Write}
		}
		order, dirty := []int{}, []int{}
		for key, p := range pages {
			order = append(order, key)
			if p.dirty {
				dirty = append(dirty, key)
			}
		}
		sort.Slice(order, func(i, j int) bool { return pages[order[i]].age < pages[order[j]].age })
		sort.Ints(dirty)
		return map[string]any{"misses": misses, "writebacks": writes, "order": order, "dirty": dirty}
	case "taint-dataflow":
		nodes := codingQuestion[[]benchmarkTaintNode](t, item)
		out := make([]int, len(nodes))
		for bit := 1; bit <= 8; bit *= 2 {
			queue, seen := []int{}, map[int]bool{}
			for i, node := range nodes {
				if node.Sources&bit != 0 && node.Kill&bit == 0 {
					seen[i] = true
					queue = append(queue, i)
				}
			}
			for head := 0; head < len(queue); head++ {
				parent := queue[head]
				for child, node := range nodes {
					for _, input := range node.Parents {
						if input == parent && !seen[child] && node.Kill&bit == 0 {
							seen[child] = true
							queue = append(queue, child)
						}
					}
				}
			}
			for node := range seen {
				out[node] += bit
			}
		}
		return out
	case "bytecode-control-flow":
		d := codingQuestion[benchmarkBytecodeData](t, item)
		n, a, b, sum := d.Locals[0], d.Locals[1], d.Locals[2], d.Locals[3]
		steps := 3
		for counter := n; counter > 0; counter-- {
			steps += 15
			if counter%2 == 1 {
				steps += 8
				sum += a
				a += b
			}
			b ^= a
		}
		return map[string]any{"locals": []int{0, a, b, sum}, "steps": steps}
	case "signed-overflow-trace":
		d := codingQuestion[benchmarkOverflowData](t, item)
		modulus, value, out := 1<<d.Bits, d.Initial, []int{}
		for _, op := range d.Operations {
			switch op.Op {
			case "add":
				value += op.Arg
			case "mul":
				value *= op.Arg
			case "xor":
				unsigned, result := (value%modulus+modulus)%modulus, 0
				for place := 1; place < modulus; place *= 2 {
					if unsigned/place%2 != op.Arg/place%2 {
						result += place
					}
				}
				value = result
			case "shr":
				divisor := 1 << op.Arg
				if value < 0 {
					value = -((-value + divisor - 1) / divisor)
				} else {
					value /= divisor
				}
			case "ushr":
				value = ((value%modulus + modulus) % modulus) / (1 << op.Arg)
			}
			value = (value%modulus + modulus) % modulus
			if value >= modulus/2 {
				value -= modulus
			}
			out = append(out, value)
		}
		return out
	case "struct-layout-padding":
		fields := codingQuestion[[]benchmarkLayoutField](t, item)
		offsets, cursor, padding, alignment := []int{}, 0, 0, 1
		for _, field := range fields {
			for cursor%field.Size != 0 {
				cursor++
				padding++
			}
			offsets = append(offsets, cursor)
			for element := 0; element < field.Count; element++ {
				cursor += field.Size
			}
			if field.Size > alignment {
				alignment = field.Size
			}
		}
		for cursor%alignment != 0 {
			cursor++
			padding++
		}
		return map[string]any{"offsets": offsets, "size": cursor, "padding": padding}
	case "repair-lower-bound":
		d := codingQuestion[benchmarkLowerBoundData](t, item)
		comparison := [2][2]string{{"lt", "le"}, {"gt", "ge"}}
		order, strict := 0, 0
		if d.Descending {
			order = 1
		}
		if d.Strict {
			strict = 1
		}
		return map[string]any{"comparison": comparison[order][strict], "advance": 1, "retreat": 0, "result_offset": 0}
	case "repair-range-sum":
		d := codingQuestion[benchmarkRangePatchData](t, item)
		end := 0
		if d.IncludeEnd {
			end = 1
		}
		return map[string]any{"start_offset": -d.IndexBase, "end_offset": end - d.IndexBase, "wrap_comparison": "lt", "wrap_factor": 1}
	case "minimal-bug-witness":
		d := codingQuestion[benchmarkBugData](t, item)
		for length := 1; length <= d.MaxLength; length++ {
			combinations, alphabet := 1, d.Maximum-d.Minimum+1
			for i := 0; i < length; i++ {
				combinations *= alphabet
			}
			for code := 0; code < combinations; code++ {
				values, digits := make([]int, length), code
				for i := length - 1; i >= 0; i-- {
					values[i], digits = d.Minimum+digits%alphabet, digits/alphabet
				}
				if codingWindowResults(values, d.Target) {
					return values
				}
			}
		}
		t.Fatal("public domain has no counterexample")
	case "sql-null-antijoin":
		d := codingQuestion[benchmarkSQLData](t, item)
		in, exists := []int{}, []int{}
		for _, l := range d.Left {
			// AND of l.key != each active r.key, using 1=true,0=false,-1=unknown.
			conjunction, found := 1, false
			for _, r := range d.Right {
				if !r.Active {
					continue
				}
				term := 1
				if l.Null || r.Null {
					term = -1
				} else if l.Key == r.Key {
					term, found = 0, true
				}
				if term == 0 || conjunction == 0 {
					conjunction = 0
				} else if term == -1 {
					conjunction = -1
				}
			}
			if conjunction == 1 {
				in = append(in, l.ID)
			}
			if !found {
				exists = append(exists, l.ID)
			}
		}
		return map[string]any{"not_in": in, "not_exists": exists}
	default:
		return codingAlgorithmOracle(t, item)
	}
	return nil
}

// Recompute both results from prefix sums, without either production routine.
func codingWindowResults(a []int, target int) bool {
	prefix := make([]int, len(a)+1)
	for i, value := range a {
		prefix[i+1] = prefix[i] + value
	}
	want, got, left := len(a)+1, len(a)+1, 0
	for size := 1; size <= len(a); size++ {
		for start := 0; start+size <= len(a); start++ {
			if prefix[start+size]-prefix[start] >= target && size < want {
				want = size
			}
		}
	}
	for end := 1; end <= len(a); end++ {
		for prefix[end]-prefix[left] >= target {
			got = min(got, end-left)
			left++
		}
	}
	return got != want
}

func TestModelBenchmarkCodingWeightsAndDifficulty(t *testing.T) {
	counts, difficulty, weights := map[string]int{}, map[int]int{}, map[string]float64{}
	for _, item := range GenerateModelBenchmarkComparisonItems("coding-v4", 1) {
		counts[item.Category]++
		difficulty[item.Difficulty]++
		weights[item.Category] += modelBenchmarkItemWeight(item)
	}
	// D7 新增 8 个题型：algorithms +2、code +2、planning +2、logic +1、math +1。
	require.Equal(t, 32, counts["code"])
	require.Equal(t, 25, counts["algorithms"])
	require.Equal(t, 11, counts["planning"])
	require.Equal(t, 12, counts["logic"])
	require.Equal(t, 7, counts["math"])
	require.Equal(t, 49, difficulty[5])
	require.Equal(t, 18, difficulty[6])
	require.Equal(t, 8, difficulty[7])
	require.InDelta(t, .28, weights["code"], 1e-12)
	require.InDelta(t, .18, weights["algorithms"], 1e-12)
	total := 0.0
	for _, weight := range weights {
		total += weight
	}
	require.InDelta(t, 1.0, total, 1e-12)
	for category, want := range map[string]float64{"code": 28, "algorithms": 18} {
		run := &ModelBenchmarkRun{ItemCount: len(modelBenchmarkFamilies), SampleCount: 1}
		results := []ModelBenchmarkRunItem{}
		for _, item := range GenerateModelBenchmarkComparisonItems("coding-v4", 1) {
			raw, err := json.Marshal(map[string]any{"answer": item.Expected})
			require.NoError(t, err)
			result := ModelBenchmarkScoreAnswer(item, string(raw))
			if item.Category != category {
				result.Status, result.Score, result.EarnedPoints = "incorrect", 0, 0
			}
			results = append(results, result)
		}
		AggregateModelBenchmarkRun(run, results)
		require.NotNil(t, run.IntelligenceScore)
		require.InDelta(t, want, *run.IntelligenceScore, 1e-9, fmt.Sprintf("category %s", category))
	}
}
