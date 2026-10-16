//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelBenchmarkExtendedIndependentOracles(t *testing.T) {
	for _, family := range modelBenchmarkExtendedFamilies {
		t.Run(family.id, func(t *testing.T) {
			for seed := 1; seed <= 128; seed++ {
				for variant := 1; variant <= 2; variant++ {
					prompt, expected := generateModelBenchmarkExtended(family, newModelBenchmarkRNG(uint64(seed*71+variant)), variant)
					item := ModelBenchmarkItem{ID: family.id, FamilyID: family.id, Category: family.category, Difficulty: family.difficulty, Checker: family.checker, Prompt: prompt, Expected: expected}
					want := benchmarkExtendedOracle(t, item)
					if family.checker == "number_tolerance" {
						require.InDelta(t, want, expected, 1e-10, prompt)
					} else if family.checker != "witness" {
						a, _ := json.Marshal(want)
						b, _ := json.Marshal(expected)
						require.JSONEq(t, string(a), string(b), prompt)
					}
					raw, err := json.Marshal(map[string]any{"answer": want})
					require.NoError(t, err)
					result := ModelBenchmarkScoreAnswer(item, string(raw))
					require.Equal(t, "correct", result.Status, prompt+": "+result.FailureReason)
					// Exercise a deliberately wrong response, independent of the reference.
					wrong := any(nil)
					switch v := want.(type) {
					case int:
						wrong = v + 1000
					case float64:
						wrong = v + 1
					case bool:
						wrong = !v
					case string:
						wrong = v + "_wrong"
					case map[string]any:
						wrong = map[string]any{"wrong": true}
					default:
						wrong = "not-an-array"
					}
					raw, _ = json.Marshal(map[string]any{"answer": wrong})
					require.NotEqual(t, "correct", ModelBenchmarkScoreAnswer(item, string(raw)).Status, family.id)
				}
			}
		})
	}
}

// This parser reads the model-visible question, not hidden generator state.
func benchmarkExtendedData(t *testing.T, item ModelBenchmarkItem) map[string]any {
	t.Helper()
	var data map[string]any
	require.NoError(t, modelBenchmarkQuestionData(item, &data))
	return data
}
func bxInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	}
	panic(fmt.Sprintf("not integer: %T", v))
}
func bxInts(v any) []int {
	a := v.([]any)
	out := []int{}
	for _, x := range a {
		out = append(out, bxInt(x))
	}
	return out
}
func bxRows(v any) []map[string]any {
	out := []map[string]any{}
	for _, row := range v.([]any) {
		out = append(out, row.(map[string]any))
	}
	return out
}

func benchmarkExtendedOracle(t *testing.T, item ModelBenchmarkItem) any {
	t.Helper()
	d := benchmarkExtendedData(t, item)
	switch item.FamilyID {
	case "affine-rule":
		candidates := map[int]bool{}
		for a := 2; a <= 7; a++ {
			for b := -7; b <= 8; b++ {
				ok := true
				for _, v := range d["examples"].([]any) {
					p := bxInts(v)
					ok = ok && a*p[0]+b == p[1]
				}
				if ok {
					candidates[a*bxInt(d["query"])+b] = true
				}
			}
		}
		require.Len(t, candidates, 1)
		for x := range candidates {
			return x
		}
	case "symbolic-substitution":
		mappings := []string{}
		benchmarkAuditPermutations(4, func(order []int) {
			mapping := ""
			for _, i := range order {
				mapping += string(byte('w' + i))
			}
			valid := true
			for input, output := range d["examples"].(map[string]any) {
				result := ""
				for _, c := range input {
					result += string(mapping[c-'A'])
				}
				valid = valid && result == output
			}
			if valid {
				mappings = append(mappings, mapping)
			}
		})
		require.Len(t, mappings, 1)
		answer := ""
		for _, c := range d["query"].(string) {
			answer += string(mappings[0][c-'A'])
		}
		return answer
	case "sequence-transduction":
		apply := func(a []int, reverse, k, delta int) []int {
			out := []int{}
			for i := range a {
				index := (i + k) % len(a)
				if reverse == 1 {
					index = len(a) - 1 - index
				}
				out = append(out, a[index]+delta)
			}
			return out
		}
		outputs := map[string][]int{}
		for reverse := 0; reverse < 2; reverse++ {
			for k := 0; k <= 2; k++ {
				for delta := -2; delta <= 2; delta++ {
					valid := true
					for _, example := range bxRows(d["examples"]) {
						valid = valid && reflect.DeepEqual(apply(bxInts(example["input"]), reverse, k, delta), bxInts(example["output"]))
					}
					if valid {
						out := apply(bxInts(d["query"]), reverse, k, delta)
						outputs[fmt.Sprint(out)] = out
					}
				}
			}
		}
		require.Len(t, outputs, 1, "examples must determine a unique output")
		for _, out := range outputs {
			return out
		}
	case "grid-transform":
		// Invert the destination coordinate for each of the eight symmetries.
		transform := func(a []int, n, rotation, mirror int) []int {
			out := []int{}
			for row := 0; row < n; row++ {
				for col := 0; col < n; col++ {
					r, c := row, col
					if mirror == 1 {
						c = n - 1 - c
					}
					switch rotation {
					case 1:
						r, c = n-1-c, r
					case 2:
						r, c = n-1-r, n-1-c
					case 3:
						r, c = c, n-1-r
					}
					out = append(out, a[r*n+c])
				}
			}
			return out
		}
		outputs := [][]int{}
		for rotation := 0; rotation < 4; rotation++ {
			for mirror := 0; mirror < 2; mirror++ {
				if reflect.DeepEqual(transform(bxInts(d["example_input"]), 3, rotation, mirror), bxInts(d["example_output"])) {
					outputs = append(outputs, transform(bxInts(d["query"]), 5, rotation, mirror))
				}
			}
		}
		require.Len(t, outputs, 1)
		return outputs[0]
	case "relation-composition":
		value := bxInt(d["start"])
		for _, operation := range d["operations"].([]any) {
			mapping := d[operation.(string)].([]any)
			value = bxInt(mapping[value])
		}
		return value
	case "finite-state-inference":
		outputs := map[int]bool{}
		for code := 0; code < 16; code++ {
			table := [2][2]int{{code & 1, code >> 1 & 1}, {code >> 2 & 1, code >> 3 & 1}}
			run := func(input string) int {
				state := 0
				for _, c := range input {
					state = table[state][c-'a']
				}
				return state
			}
			valid := true
			for _, example := range bxRows(d["examples"]) {
				valid = valid && run(example["input"].(string)) == bxInt(example["output"])
			}
			if valid {
				outputs[run(d["query"].(string))] = true
			}
		}
		require.Len(t, outputs, 1, "examples must determine the answer even if internal tables are equivalent")
		for output := range outputs {
			return output
		}
	case "relational-order":
		objects := d["objects"].([]any)
		query := d["query"].([]any)
		before, after := false, false
		benchmarkAuditPermutations(len(objects), func(order []int) {
			position := map[string]int{}
			for i, n := range order {
				position[objects[n].(string)] = i
			}
			for _, raw := range d["before"].([]any) {
				e := raw.([]any)
				if position[e[0].(string)] >= position[e[1].(string)] {
					return
				}
			}
			if position[query[0].(string)] < position[query[1].(string)] {
				before = true
			} else {
				after = true
			}
		})
		if before && after {
			return "unknown"
		}
		if before {
			return "before"
		}
		return "after"
	case "conditional-rules":
		facts := bxInts(d["facts"])
		rules := bxRows(d["rules"])
		var can func(int, map[int]bool) bool
		can = func(n int, visiting map[int]bool) bool {
			for _, f := range facts {
				if f == n {
					return true
				}
			}
			if visiting[n] {
				return false
			}
			visiting[n] = true
			defer delete(visiting, n)
			for _, rule := range rules {
				if bxInt(rule["then"]) == n {
					valid := true
					for _, p := range bxInts(rule["if"]) {
						valid = valid && can(p, visiting)
					}
					if valid {
						return true
					}
				}
			}
			return false
		}
		result := []int{}
		for n := 0; n < 10; n++ {
			if can(n, map[int]bool{}) {
				result = append(result, n)
			}
		}
		return result
	case "quantified-logic":
		yes, no := 0, 0
		for _, v := range d["rows"].([]any) {
			row := v.([]any)
			if strings.HasPrefix(d["query"].(string), "forall") {
				if row[0].(bool) && !row[1].(bool) {
					no++
				}
			} else {
				if row[0].(bool) && row[1].(bool) {
					yes++
				}
			}
		}
		if strings.HasPrefix(d["query"].(string), "forall") {
			return no == 0
		}
		return yes > 0
	case "constraint-entailment":
		truth := map[bool]bool{}
		for encoded := 0; encoded < 64; encoded++ {
			x, y, z := encoded%4, encoded/4%4, encoded/16
			if x+y <= bxInt(d["c"]) && y+z >= bxInt(d["d"]) && x != z {
				truth[x < z] = true
			}
		}
		if len(truth) == 0 {
			return "inconsistent"
		}
		if len(truth) == 2 {
			return "unknown"
		}
		if truth[true] {
			return "entailed"
		}
		return "refuted"
	case "inconsistent-statements":
		claims := d["claims"].([]any)
		holds := func(raw any, x int) bool {
			c := raw.([]any)
			v := bxInt(c[1])
			switch c[0] {
			case "ge":
				return x >= v
			case "le":
				return x <= v
			case "eq":
				return x == v
			case "ne":
				return x != v
			}
			return false
		}
		var result []int
		var choose func([]int, int, int)
		choose = func(chosen []int, next, remaining int) {
			if result != nil {
				return
			}
			if remaining == 0 {
				for x := 0; x <= 10; x++ {
					valid := true
					for _, i := range chosen {
						valid = valid && holds(claims[i], x)
					}
					if valid {
						return
					}
				}
				result = append([]int{}, chosen...)
				return
			}
			for i := next; i < len(claims); i++ {
				choose(append(chosen, i), i+1, remaining-1)
			}
		}
		for size := 1; size <= len(claims) && result == nil; size++ {
			choose(nil, 0, size)
		}
		require.NotNil(t, result)
		return result
	case "missing-information":
		possible := map[int]bool{}
		for x := 0; x <= bxInt(d["domain_max"]); x++ {
			for y := 0; y <= bxInt(d["domain_max"]); y++ {
				if x+y != bxInt(d["total"]) {
					continue
				}
				known, ok := d["known_x"]
				if !ok || x == bxInt(known) {
					possible[x] = true
				}
			}
		}
		status := "unknown"
		var value any
		if len(possible) == 0 {
			status = "inconsistent"
		}
		if len(possible) == 1 {
			status = "unique"
			for x := range possible {
				value = x
			}
		}
		return map[string]any{"status": status, "value": value}
	case "causal-intervention":
		total, denominator := 0, 0
		for u := 0; u <= 1; u++ {
			weight := bxInt(d["prior_percent"])
			if u == 0 {
				weight = 100 - weight
			}
			x := u
			if d["operation"] == "intervene" {
				x = bxInt(d["value"])
			} else if x != bxInt(d["value"]) {
				continue
			}
			y := bxInt(d["a"])*x + bxInt(d["b"])*u + bxInt(d["c"])
			total += weight * y
			denominator += weight
		}
		return float64(total) / float64(denominator)
	case "causal-counterfactual":
		candidates := []int{}
		for u := -20; u <= 20; u++ {
			if bxInt(d["a"])*bxInt(d["observed_x"])+bxInt(d["b"])*u+bxInt(d["c"]) == bxInt(d["observed_y"]) {
				candidates = append(candidates, bxInt(d["a"])*bxInt(d["new_x"])+bxInt(d["b"])*u+bxInt(d["c"]))
			}
		}
		require.Len(t, candidates, 1)
		return candidates[0]
	case "bayes-diagnosis":
		// A population of 10,000 makes all generated percentages exact counts.
		diseased := bxInt(d["prior_percent"]) * 100
		truePositives := diseased * bxInt(d["sensitivity_percent"]) / 100
		falsePositives := (10000 - diseased) * (100 - bxInt(d["specificity_percent"])) / 100
		return float64(truePositives) / float64(truePositives+falsePositives)
	case "simpson-confounding":
		rows := bxRows(d["rows"])
		success, total := map[string]int{}, map[string]int{}
		byStratum := map[string]map[string]float64{}
		for _, row := range rows {
			g, s := row["group"].(string), row["stratum"].(string)
			success[g] += bxInt(row["success"])
			total[g] += bxInt(row["total"])
			if byStratum[s] == nil {
				byStratum[s] = map[string]float64{}
			}
			byStratum[s][g] = float64(bxInt(row["success"])) / float64(bxInt(row["total"]))
		}
		winners := map[string]bool{}
		for _, rates := range byStratum {
			if rates["A"] > rates["B"] {
				winners["A"] = true
			} else {
				winners["B"] = true
			}
		}
		require.Len(t, winners, 1)
		within := ""
		for winner := range winners {
			within = winner
		}
		overall := "B"
		if success["A"]*total["B"] > success["B"]*total["A"] {
			overall = "A"
		}
		return map[string]any{"within_groups": within, "overall": overall}
	case "ledger-reconciliation":
		credits, debits := 0, 0
		for _, row := range bxRows(d["entries"]) {
			if row["void"].(bool) {
				continue
			}
			if row["kind"] == "payment" {
				credits += bxInt(row["cents"])
			} else {
				debits += bxInt(row["cents"])
			}
		}
		return credits - debits
	case "multi-hop-records":
		key := d["query_employee"].(string)
		for _, table := range []string{"employees", "departments", "sites"} {
			key = d[table].(map[string]any)[key].(string)
		}
		return key
	case "unit-conversion":
		distances := []float64{float64(bxInt(d["distance_km"])) * 1000, float64(bxInt(d["distance_m"]))}
		times := []float64{float64(bxInt(d["time_minutes"])) * 60, float64(bxInt(d["time_seconds"]))}
		return (distances[0] + distances[1]) / (times[0] + times[1])
	case "weighted-rate":
		numerator, denominator := 0.0, 0.0
		for _, raw := range d["groups"].([]any) {
			group := bxInts(raw)
			weight := float64(group[1])
			numerator += float64(group[0]) / weight * weight
			denominator += weight
		}
		return numerator / denominator
	case "long-context-join":
		rows := bxRows(d["records"])
		sort.Slice(rows, func(i, j int) bool { return bxInt(rows[i]["version"]) > bxInt(rows[j]["version"]) })
		seen := map[int]bool{}
		answer := 0
		for _, row := range rows {
			warehouse := bxInt(row["warehouse"])
			if row["product"] != d["query"] || row["cancelled"].(bool) || seen[warehouse] {
				continue
			}
			seen[warehouse] = true
			answer += bxInt(row["quantity"])
		}
		return answer
	case "versioned-policy":
		rows := bxRows(d["policies"])
		sort.Slice(rows, func(i, j int) bool { return bxInt(rows[i]["effective_day"]) > bxInt(rows[j]["effective_day"]) })
		for _, row := range rows {
			if bxInt(row["effective_day"]) <= bxInt(d["order_day"]) {
				return bxInt(d["goods_cents"]) / 100 * bxInt(row["refund_percent"])
			}
		}
	case "trace-loop-boundary":
		visited := []int{}
		sum := bxInt(d["initial"])
		for value := bxInt(d["start"]); value <= bxInt(d["end"]); value++ {
			if (value-bxInt(d["start"]))%bxInt(d["step"]) == 0 && value%bxInt(d["mod"]) != 0 {
				visited = append(visited, value)
				sum += value
			}
		}
		return map[string]any{"sum": sum, "visited": visited}
	case "expression-evaluation":
		stack := []int{}
		for _, raw := range d["tokens"].([]any) {
			token := raw.(string)
			if n, err := strconv.Atoi(token); err == nil {
				stack = append(stack, n)
			} else {
				b, a := stack[len(stack)-1], stack[len(stack)-2]
				stack = stack[:len(stack)-2]
				switch token {
				case "+":
					stack = append(stack, a+b)
				case "-":
					stack = append(stack, a-b)
				case "*":
					stack = append(stack, a*b)
				}
			}
		}
		require.Len(t, stack, 1)
		return stack[0]
	case "bug-counterexample":
		for index, c := range bxRows(d["cases"]) {
			a, target := bxInts(c["values"]), bxInt(c["target"])
			want := len(a)
			for i, v := range a {
				if v >= target {
					want = i
					break
				}
			}
			lo, hi := 0, len(a)
			for lo < hi {
				m := (lo + hi) / 2
				if a[m] >= target {
					hi = m - 1
				} else {
					lo = m + 1
				}
			}
			if lo != want {
				return index
			}
		}
	case "proof-error":
		previous := bxInts(d["steps"].([]any)[0])
		for i, raw := range d["steps"].([]any)[1:] {
			step := bxInts(raw)
			if (previous[2]-previous[1])*step[0] != (step[2]-step[1])*previous[0] {
				return i + 1
			}
			previous = step
		}
	case "dependency-schedule":
		jobs := bxRows(d["jobs"])
		memo := map[int]int{}
		var finish func(int) int
		finish = func(i int) int {
			if value, ok := memo[i]; ok {
				return value
			}
			start := 0
			for _, parent := range bxInts(jobs[i]["parents"]) {
				value := finish(parent)
				if value > start {
					start = value
				}
			}
			memo[i] = start + bxInt(jobs[i]["duration"])
			return memo[i]
		}
		answer := 0
		for i := range jobs {
			value := finish(i)
			if value > answer {
				answer = value
			}
		}
		return answer
	case "resource-allocation":
		items := bxRows(d["items"])
		bestValue := -1
		best := []int{}
		var visit func(int, int, int, int, []int)
		visit = func(i, cost, weight, value int, indices []int) {
			if cost > bxInt(d["budget"]) || weight > bxInt(d["capacity"]) {
				return
			}
			if i == len(items) {
				if value > bestValue {
					bestValue, best = value, append([]int{}, indices...)
				}
				return
			}
			visit(i+1, cost, weight, value, indices)
			visit(i+1, cost+bxInt(items[i]["cost"]), weight+bxInt(items[i]["weight"]), value+bxInt(items[i]["value"]), append(indices, i))
		}
		visit(0, 0, 0, 0, nil)
		return best
	case "route-with-windows":
		// Subset DP stores earliest completion for every (visited,last) state.
		type state struct{ mask, last int }
		cost := map[state]int{{0, 0}: 0}
		path := map[state][]int{{0, 0}: {}}
		best := -1
		order := []int{}
		for mask := 0; mask < 16; mask++ {
			for last := 0; last <= 4; last++ {
				s := state{mask, last}
				clock, ok := cost[s]
				if !ok {
					continue
				}
				if mask == 15 {
					if best < 0 || clock < best {
						best, order = clock, path[s]
					}
					continue
				}
				for next := 1; next <= 4; next++ {
					if mask>>(next-1)&1 != 0 {
						continue
					}
					travel := bxInts(d["travel"].([]any)[last])[next]
					window := bxInts(d["windows"].([]any)[next-1])
					arrival := clock + travel
					if arrival < window[0] {
						arrival = window[0]
					}
					if arrival > window[1] {
						continue
					}
					finish := arrival + bxInt(d["service"])
					ns := state{mask | 1<<(next-1), next}
					old, seen := cost[ns]
					if !seen || finish < old {
						cost[ns] = finish
						path[ns] = append(append([]int{}, path[s]...), next)
					}
				}
			}
		}
		return map[string]any{"order": order, "finish_time": best}
	case "inventory-policy":
		arrivals := map[int]int{}
		stock := bxInt(d["initial"])
		out := []int{}
		for day, demand := range bxInts(d["demand"]) {
			stock += arrivals[day]
			delete(arrivals, day)
			sold := demand
			if stock < sold {
				sold = stock
			}
			stock -= sold
			if stock <= bxInt(d["threshold"]) && len(arrivals) == 0 {
				arrivals[day+bxInt(d["lead"])] = bxInt(d["batch"])
			}
			out = append(out, stock)
		}
		return out
	case "distractor-extraction":
		for _, row := range bxRows(d["rows"]) {
			if row["role"] == "record" && row["id"] == "target" {
				return row["token"]
			}
		}
	case "nested-constraints":
		rows := bxRows(d["rows"])
		out := []any{}
		for len(out) < bxInt(d["k"]) {
			index := -1
			for i, row := range rows {
				if !row["active"].(bool) || bxInt(row["score"]) < bxInt(d["threshold"]) {
					continue
				}
				if index < 0 || bxInt(row["score"]) > bxInt(rows[index]["score"]) || bxInt(row["score"]) == bxInt(rows[index]["score"]) && row["id"].(string) < rows[index]["id"].(string) {
					index = i
				}
			}
			if index < 0 {
				break
			}
			out = append(out, map[string]any{"id": rows[index]["id"], "score": rows[index]["score"]})
			rows[index]["active"] = false
		}
		return map[string]any{"count": len(out), "rows": out}
	}
	t.Fatalf("independent oracle unavailable: %s", item.FamilyID)
	return nil
}

func TestModelBenchmarkPublicFormsAndSampling(t *testing.T) {
	a := GenerateModelBenchmarkComparisonItems("study-2026", 1)
	b := GenerateModelBenchmarkComparisonItems("study-2026", 1)
	require.Equal(t, a, b)
	require.Len(t, a, 56)
	hashes := map[string]map[string]bool{}
	for form := 1; form <= 32; form++ {
		for _, item := range GenerateModelBenchmarkComparisonItems(fmt.Sprintf("form-%d", form), form%3+1) {
			if hashes[item.ID] == nil {
				hashes[item.ID] = map[string]bool{}
			}
			hashes[item.ID][item.PromptHash] = true
		}
	}
	for id, values := range hashes {
		require.Greater(t, len(values), 1, id)
	}
	totals := map[string]float64{}
	for _, item := range a {
		totals[item.Category] += modelBenchmarkItemWeight(item)
	}
	for category, weight := range modelBenchmarkCategoryWeights {
		require.InDelta(t, weight, totals[category], 1e-12, category)
	}
}

func TestModelBenchmarkCoverageUsesPlannedItems(t *testing.T) {
	run := &ModelBenchmarkRun{ItemCount: 56, SampleCount: 1}
	items := []ModelBenchmarkRunItem{{Category: "math", Status: "correct", Score: 1, EarnedPoints: 1, PossiblePoints: 1}}
	AggregateModelBenchmarkRun(run, items)
	require.Equal(t, 56, run.ItemCount)
	require.Equal(t, "partial", run.Status)
	require.InDelta(t, 100.0/56, run.Quality.CompletionRate, 1e-9)
	require.False(t, run.Quality.RankingEligible)
	all := []ModelBenchmarkRunItem{}
	for _, item := range GenerateModelBenchmarkComparisonItems("coverage", 1) {
		raw, _ := json.Marshal(map[string]any{"answer": item.Expected})
		all = append(all, ModelBenchmarkScoreAnswer(item, string(raw)))
	}
	AggregateModelBenchmarkRun(run, all)
	require.True(t, run.Quality.RankingEligible)
	require.Equal(t, 100.0, run.Quality.CompletionRate)
	require.InDelta(t, 100.0, *run.IntelligenceScore, 1e-9)
	for i := range all {
		if all[i].Category == "causal" {
			all[i].Status = "execution_failed"
		}
	}
	AggregateModelBenchmarkRun(run, all)
	require.Greater(t, run.Quality.CompletionRate, 90.0)
	require.False(t, run.Quality.RankingEligible, "one missing domain must prevent ranking even at high overall coverage")
}

func TestModelBenchmarkBudgetIncludesAllSamples(t *testing.T) {
	plan := &ModelBenchmarkPlan{SampleCount: 3, TimeoutSeconds: 600}
	require.Greater(t, ModelBenchmarkRunBudget(plan), time.Duration(56*3*600)*time.Second)
}

func TestModelBenchmarkEquivalentWitnesses(t *testing.T) {
	item := ModelBenchmarkItem{FamilyID: "resource-allocation", Checker: "witness", Expected: []int{0}, Prompt: modelBenchmarkDataQuestion("select", map[string]any{"items": []benchmarkResourceItem{{1, 1, 5}, {1, 1, 5}}, "budget": 1, "capacity": 1})}
	require.Equal(t, 1.0, ModelBenchmarkScoreAnswer(item, `{"answer":[1]}`).Score)
	require.Zero(t, ModelBenchmarkScoreAnswer(item, `{"answer":[0,1]}`).Score)
	require.Zero(t, ModelBenchmarkScoreAnswer(item, `{"answer":[1,1]}`).Score)
}

func TestModelBenchmarkComparisonKeyValidation(t *testing.T) {
	plan := &ModelBenchmarkPlan{SampleCount: 1, MaxOutputTokens: 2048, TimeoutSeconds: 90, MaxResults: 50, ComparisonKey: "study.A-1"}
	require.NoError(t, validateModelBenchmarkPlanLimits(plan))
	plan.ComparisonKey = "unsafe key"
	require.ErrorIs(t, validateModelBenchmarkPlanLimits(plan), ErrModelBenchmarkInvalidPlan)
}
