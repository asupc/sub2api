//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These oracles read the question, never the generator's Expected value. They use
// enumeration, direct simulation, or a different algorithm from the generator.
func TestModelBenchmarkIndependentAnswers(t *testing.T) {
	for _, family := range modelBenchmarkCoreFamilies {
		t.Run(family.id, func(t *testing.T) {
			for run := int64(1); run <= 128; run++ {
				for variant := 1; variant <= 2; variant++ {
					rng := newModelBenchmarkRNG(modelBenchmarkSeed(17, run, family.id, variant))
					prompt, expected := generateModelBenchmarkInstance(family, rng, variant)
					want := benchmarkAuditAnswer(t, family.id, prompt)
					gotJSON, err := json.Marshal(expected)
					require.NoError(t, err)
					wantJSON, err := json.Marshal(want)
					require.NoError(t, err)
					if family.checker == "number_tolerance" {
						require.InDelta(t, want, expected, 1e-12, prompt)
					} else {
						require.JSONEq(t, string(wantJSON), string(gotJSON), prompt)
					}
					item := ModelBenchmarkItem{ID: family.id, Category: family.category, Difficulty: family.difficulty, Checker: family.checker, Expected: expected}
					result := ModelBenchmarkScoreAnswer(item, `{"answer":`+string(wantJSON)+`}`)
					require.Equal(t, "correct", result.Status, prompt+": "+result.FailureReason)
				}
			}
		})
	}
}

var benchmarkAuditPatterns = map[string]*regexp.Regexp{}

func benchmarkAuditMatch(t *testing.T, pattern, text string) []string {
	t.Helper()
	re := benchmarkAuditPatterns[pattern]
	if re == nil {
		re = regexp.MustCompile(pattern)
		benchmarkAuditPatterns[pattern] = re
	}
	matches := re.FindStringSubmatch(text)
	require.NotNil(t, matches, "question does not define the required inputs: %s", text)
	return matches[1:]
}

func benchmarkAuditInts(t *testing.T, pattern, text string) []int {
	t.Helper()
	matches := benchmarkAuditMatch(t, pattern, text)
	values := make([]int, len(matches))
	for i, value := range matches {
		n, err := strconv.Atoi(value)
		require.NoError(t, err)
		values[i] = n
	}
	return values
}

func benchmarkAuditArray(text string) []int {
	tokens := regexp.MustCompile(`-?\d+`).FindAllString(text, -1)
	values := make([]int, len(tokens))
	for i, token := range tokens {
		values[i], _ = strconv.Atoi(token)
	}
	return values
}

func benchmarkAuditAnswer(t *testing.T, family, prompt string) any {
	t.Helper()
	switch family {
	case "modular-exponentiation":
		v := benchmarkAuditInts(t, `计算 (\d+)\^(\d+) mod (\d+)`, prompt)
		base, exponent, modulus := int64(v[0]), v[1], int64(v[2])
		result := int64(1)
		for ; exponent > 0; exponent >>= 1 {
			if exponent&1 != 0 {
				result = result * base % modulus
			}
			base = base * base % modulus
		}
		return result
	case "crt-remainder":
		v := benchmarkAuditInts(t, `x mod (\d+) = (\d+) 且 x mod (\d+) = (\d+)`, prompt)
		for x := v[1]; x < v[0]*v[2]; x += v[0] {
			if x%v[2] == v[3] {
				return x
			}
		}
		t.Fatal("no CRT solution")
	case "constrained-permutations":
		v := benchmarkAuditInts(t, `计算 (\d+) 个不同元素的排列数，要求前 (\d+) 个`, prompt)
		n, fixed := v[0], v[1]
		ways := make([]int, 1<<n)
		ways[0] = 1
		for mask := 0; mask < 1<<n; mask++ {
			position := bits.OnesCount(uint(mask))
			for element := 0; element < n; element++ {
				if mask&(1<<element) == 0 && (position >= fixed || position == element) {
					ways[mask|1<<element] += ways[mask]
				}
			}
		}
		return ways[len(ways)-1]
	case "shortest-path":
		require.Contains(t, prompt, "字典序最小最短节点序列")
		const infinity = 1000000
		distance := [5][5]int{}
		edges := [5][5]int{}
		for i := 0; i < 5; i++ {
			for j := 0; j < 5; j++ {
				if i != j {
					distance[i][j] = infinity
				}
			}
		}
		for _, edge := range regexp.MustCompile(`([A-E])-([A-E]):(\d+)`).FindAllStringSubmatch(prompt, -1) {
			a, b := int(edge[1][0]-'A'), int(edge[2][0]-'A')
			w, _ := strconv.Atoi(edge[3])
			distance[a][b], distance[b][a], edges[a][b], edges[b][a] = w, w, w, w
		}
		for k := 0; k < 5; k++ {
			for i := 0; i < 5; i++ {
				for j := 0; j < 5; j++ {
					distance[i][j] = min(distance[i][j], distance[i][k]+distance[k][j])
				}
			}
		}
		path := []string{"A"}
		for node := 0; node != 4; {
			next := -1
			for j := 0; j < 5; j++ {
				if edges[node][j] > 0 && distance[node][4] == edges[node][j]+distance[j][4] {
					next = j
					break
				}
			}
			require.NotEqual(t, -1, next)
			node = next
			path = append(path, string(rune('A'+node)))
		}
		return path
	case "topological-order":
		require.Contains(t, prompt, "小根堆版 Kahn")
		edges := regexp.MustCompile(`([A-E])>([A-E])`).FindAllStringSubmatch(prompt, -1)
		var best []int
		benchmarkAuditPermutations(5, func(order []int) {
			positions := [5]int{}
			for i, node := range order {
				positions[node] = i
			}
			for _, edge := range edges {
				if positions[edge[1][0]-'A'] >= positions[edge[2][0]-'A'] {
					return
				}
			}
			if best == nil || benchmarkAuditLess(order, best) {
				best = append([]int{}, order...)
			}
		})
		answer := []string{}
		for _, node := range best {
			answer = append(answer, string(rune('A'+node)))
		}
		return answer
	case "interval-dp":
		n := benchmarkAuditInts(t, `有 (\d+) 个相邻块`, prompt)[0]
		values := benchmarkAuditArray(benchmarkAuditMatch(t, `值为 (\[[^\]]+\])`, prompt)[0])
		require.Len(t, values, n)
		memo := map[string]int{}
		var remainingCost func([]int) int
		remainingCost = func(state []int) int {
			if len(state) == 1 {
				return 0
			}
			key := fmt.Sprint(state)
			if cost, ok := memo[key]; ok {
				return cost
			}
			best := math.MaxInt
			for i := 0; i+1 < len(state); i++ {
				merged := state[i] + state[i+1]
				next := append(append(append([]int{}, state[:i]...), merged), state[i+2:]...)
				best = min(best, merged+remainingCost(next))
			}
			memo[key] = best
			return best
		}
		best, first := math.MaxInt, -1
		for i := 0; i+1 < n; i++ {
			merged := values[i] + values[i+1]
			next := append(append(append([]int{}, values[:i]...), merged), values[i+2:]...)
			if cost := merged + remainingCost(next); cost < best {
				best, first = cost, i
			}
		}
		return map[string]any{"value": best, "first_merge_left_index": first}
	case "cycle-detection":
		reachable := [5][5]bool{}
		for _, edge := range regexp.MustCompile(`([1-5])>([1-5])`).FindAllStringSubmatch(prompt, -1) {
			reachable[edge[1][0]-'1'][edge[2][0]-'1'] = true
		}
		for k := 0; k < 5; k++ {
			for i := 0; i < 5; i++ {
				for j := 0; j < 5; j++ {
					reachable[i][j] = reachable[i][j] || reachable[i][k] && reachable[k][j]
				}
			}
		}
		for i := 0; i < 5; i++ {
			if reachable[i][i] {
				return true
			}
		}
		return false
	case "kmp-prefix":
		word := benchmarkAuditMatch(t, `字符串 ([a-z]+) 的 KMP`, prompt)[0]
		prefix := make([]int, len(word))
		for i := range word {
			for k := 1; k <= i; k++ {
				if word[:k] == word[i-k+1:i+1] {
					prefix[i] = k
				}
			}
		}
		return prefix
	case "sparse-matrix":
		offset := benchmarkAuditInts(t, `column \+ (\d+)\)`, prompt)[0]
		coordinates := []string{}
		for row := 1; row <= 7; row++ {
			for col := 1; col <= 7; col++ {
				if row <= 4 && col >= 3 && col <= 6 && (row*col+offset)%4 == 0 {
					coordinates = append(coordinates, fmt.Sprintf("%d,%d", row, col))
				}
			}
		}
		return coordinates
	case "token-bucket":
		require.Contains(t, prompt, "不足则拒绝本次请求且令牌数不变")
		v := benchmarkAuditInts(t, `初始有 (\d+) 个令牌，容量为 (\d+).*?补充 (\d+) 个令牌`, prompt)
		requests := benchmarkAuditArray(benchmarkAuditMatch(t, `消耗令牌：(\[[^\]]+\])`, prompt)[0])
		tokens, states := v[0], []int{}
		for _, request := range requests {
			if tokens >= request {
				tokens -= request
			}
			states = append(states, tokens)
			tokens = min(tokens+v[2], v[1])
			states = append(states, tokens)
		}
		return states
	case "modular-inverse":
		require.Contains(t, prompt, "最小非负乘法逆元")
		v := benchmarkAuditInts(t, `返回 (\d+) 在模 (\d+) 下`, prompt)
		for x := 0; x < v[1]; x++ {
			if v[0]*x%v[1] == 1 {
				return x
			}
		}
		t.Fatal("no modular inverse")
	case "markov-expected-steps":
		text := benchmarkAuditMatch(t, `概率 (\d+\.\d+)`, prompt)[0]
		probability, err := strconv.ParseFloat(text, 64)
		require.NoError(t, err)
		expectation := 0.0
		for survival := 1.0; survival > 1e-15; survival *= probability {
			expectation += survival
		}
		return expectation
	case "inclusion-exclusion":
		v := benchmarkAuditInts(t, `在 1\.\.(\d+) 中，统计不能被 (\d+)、(\d+)、(\d+)`, prompt)
		count := 0
		for i := 1; i <= v[0]; i++ {
			if i%v[1] != 0 && i%v[2] != 0 && i%v[3] != 0 {
				count++
			}
		}
		return count
	case "two-machine-schedule":
		jobs := benchmarkAuditArray(benchmarkAuditMatch(t, `作业 (\[[^\]]+\])`, prompt)[0])
		best, indices := math.MaxInt, []int(nil)
		benchmarkAuditPermutations(len(jobs), func(order []int) {
			for split := 0; split <= len(order); split++ {
				a, b := 0, 0
				for _, i := range order[:split] {
					a += jobs[i]
				}
				for _, i := range order[split:] {
					b += jobs[i]
				}
				candidate := append([]int{}, order[:split]...)
				sort.Ints(candidate)
				cost := max(a, b)
				if cost < best || cost == best && benchmarkAuditLess(candidate, indices) {
					best, indices = cost, candidate
				}
			}
		})
		return map[string]any{"makespan": best, "machine_a_indices": indices}
	case "concurrent-race":

		require.Contains(t, prompt, "没有锁、原子操作或其他同步")
		line := benchmarkAuditInts(t, `第 (\d+) 行`, prompt)[0]
		safe := strings.Contains(prompt, "先接收 ch，然后读取 x")
		defect := "data_race"
		if safe {
			defect = "none"
		}
		return map[string]any{"defect": defect, "safe": safe, "line": line}
	case "pseudocode-trace":
		n := benchmarkAuditInts(t, `从 (\d+) 开始`, prompt)[0]
		sequence := []int{}
		for n > 1 {
			if n&1 == 1 {
				n = 3*n + 1
			} else {
				n >>= 1
			}
			sequence = append(sequence, n)
		}
		return sequence
	case "loop-invariant":
		require.Contains(t, prompt, "a[i] < target")
		require.Contains(t, prompt, "a[i] >= target")
		require.Contains(t, prompt, "lo = mid + 1")
		require.Contains(t, prompt, "lo=0, hi=n")
		require.Contains(t, prompt, "否则 hi=mid")
		return benchmarkAuditMatch(t, `([ABC]): 0<=lo<=hi<=n`, prompt)[0]
	case "greedy-counterexample":
		coins := benchmarkAuditArray(benchmarkAuditMatch(t, `硬币面额为 (\[[^\]]+\])`, prompt)[0])
		total := benchmarkAuditInts(t, `金额为 (\d+)`, prompt)[0]
		minimum := make([]int, total+1)
		for amount := 1; amount <= total; amount++ {
			minimum[amount] = total + 1
			for _, coin := range coins {
				if coin <= amount {
					minimum[amount] = min(minimum[amount], minimum[amount-coin]+1)
				}
			}
		}
		sort.Ints(coins)
		amount, greedy := total, 0
		for i := len(coins) - 1; i >= 0; i-- {
			greedy += amount / coins[i]
			amount %= coins[i]
		}
		return greedy - minimum[total]
	case "truth-assignment":
		require.Contains(t, prompt, "p XOR q 为 true")
		antecedent := benchmarkAuditMatch(t, `([pq]) IMPLIES ([pq]) 为 false`, prompt)
		var solutions []map[string]any
		for _, p := range []bool{false, true} {
			for _, q := range []bool{false, true} {
				values := map[string]bool{"p": p, "q": q}
				implication := !values[antecedent[0]] || values[antecedent[1]]
				if p != q && !implication {
					solutions = append(solutions, map[string]any{"p": p, "q": q})
				}
			}
		}
		require.Len(t, solutions, 1, prompt)
		return solutions[0]
	case "strict-schema":
		literal := benchmarkAuditMatch(t, `精确返回 (\{[^\n]+\})。`, prompt)[0]
		var envelope map[string]any
		require.NoError(t, json.Unmarshal([]byte(literal), &envelope))
		require.Len(t, envelope, 1)
		answer, exists := envelope["answer"]
		require.True(t, exists)
		return answer
	case "epistemic-deduction":
		people := []string{"Ada", "Ben", "Cai"}
		different := strings.Contains(prompt, "持钥状态不同")
		holders := []string{}
		for holder, name := range people {
			ben, cai := holder == 1, holder == 2
			adaStatement := (ben != cai) == different
			benStatement := !ben
			if strings.Contains(prompt, "Ben 说自己持有钥匙") {
				benStatement = ben
			}
			if adaStatement && benStatement {
				holders = append(holders, name)
			}
		}
		require.Len(t, holders, 1, prompt)
		return holders
	case "error-chain":
		registered := strings.Fields(benchmarkAuditMatch(t, `注册 defer：(\[[^\]]+\])`, prompt)[0][1:])
		registered[len(registered)-1] = strings.TrimSuffix(registered[len(registered)-1], "]")
		order := []string{}
		for i := len(registered) - 1; i >= 0; i-- {
			order = append(order, registered[i])
		}
		return order
	case "pareto-frontier":
		require.Contains(t, prompt, "成本和延迟均越小越好")
		pairs := regexp.MustCompile(`\[(\d+),(\d+)\]`).FindAllStringSubmatch(prompt, -1)
		result := []string{}
		for _, a := range pairs {
			x, y := benchmarkAuditArray(a[0])[0], benchmarkAuditArray(a[0])[1]
			dominated := false
			for _, b := range pairs {
				u, v := benchmarkAuditArray(b[0])[0], benchmarkAuditArray(b[0])[1]
				if u <= x && v <= y && (u < x || v < y) {
					dominated = true
				}
			}
			if !dominated {
				result = append(result, fmt.Sprintf("%d,%d", x, y))
			}
		}
		return result
	case "numerical-stability":
		require.Contains(t, prompt, "sqrt(x+1)-sqrt(x)")
		require.Contains(t, prompt, "1/(sqrt(x+1)+sqrt(x))")
		require.Contains(t, prompt, "binary64")
		return benchmarkAuditMatch(t, `([ABC]): 1/\(sqrt\(x\+1\)\+sqrt\(x\)\)`, prompt)[0]
	default:
		t.Fatalf("missing independent oracle for %s", family)
	}
	return nil
}

func benchmarkAuditLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func benchmarkAuditPermutations(n int, visit func([]int)) {
	var walk func([]int, int)
	walk = func(order []int, used int) {
		if len(order) == n {
			visit(order)
			return
		}
		for i := 0; i < n; i++ {
			if used&(1<<i) == 0 {
				walk(append(order, i), used|1<<i)
			}
		}
	}
	walk(nil, 0)
}

func TestModelBenchmarkScoringAudit(t *testing.T) {
	cases := []struct {
		name, checker, response string
		expected                any
		status                  string
	}{
		{"large integer mismatch", "integer", `{"answer":9007199254740993}`, int64(9007199254740992), "incorrect"},
		{"equivalent decimal integer", "integer", `{"answer":42.0}`, 42, "correct"},
		{"large integer exact", "integer", `{"answer":9223372036854775807}`, int64(9223372036854775807), "correct"},
		{"large integer overflow", "integer", `{"answer":9223372036854775808}`, int64(9223372036854775807), "incorrect"},
		{"negative integer boundary", "integer", `{"answer":-9223372036854775808}`, int64(-9223372036854775808), "correct"},
		{"fraction is not integer", "integer", `{"answer":42.00000000000000001}`, 42, "incorrect"},
		{"tolerance inside", "number_tolerance", `{"answer":1.250000999}`, 1.25, "correct"},
		{"tolerance outside", "number_tolerance", `{"answer":1.250001001}`, 1.25, "incorrect"},
		{"numeric string rejected", "number_tolerance", `{"answer":"1.25"}`, 1.25, "incorrect"},
		{"numeric overflow rejected", "number_tolerance", `{"answer":1e1000}`, 1.25, "incorrect"},
		{"set duplicate rejected", "set", `{"answer":["Ada","Ada"]}`, []string{"Ada", "Cai"}, "incorrect"},
		{"missing final answer", "integer", `{"answer":42} {"other":42}`, 42, "format_error"},
		{"nested answer retained", "fields", `{"answer":{"answer":42}}`, map[string]any{"answer": 42}, "correct"},

		{"equivalent exponent integer", "integer", `{"answer":4.2e1}`, 42, "correct"},
		{"equivalent numeric indices", "fields", `{"answer":{"indices":[0.0,2e0]}}`, map[string]any{"indices": []int{0, 2}}, "correct"},
		{"set permutation", "set", `{"answer":["Cai","Ada"]}`, []string{"Ada", "Cai"}, "correct"},
		{"sequence permutation", "sequence", `{"answer":[2,1]}`, []int{1, 2}, "incorrect"},
		{"wrong scalar type", "exact", `{"answer":42}`, "42", "incorrect"},
		{"partial fields", "fields", `{"answer":{"value":51,"first":1}}`, map[string]any{"value": 51, "first": 0}, "partial"},
		{"last answer object", "integer", `Analysis {"answer":0} Final {"answer":42}`, 42, "correct"},
		{"braces in string", "exact", `Text {noise} {"answer":"brace } inside"}`, "brace } inside", "correct"},
		{"schema exact", "schema_only", `{"answer":{"token":"N1234","enabled":true,"count":3}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "correct"},
		{"schema extra outer field", "schema_only", `{"answer":{"token":"N1234","enabled":true,"count":3},"extra":1}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "format_error"},
		{"schema prose", "schema_only", `Here {"answer":{"token":"N1234","enabled":true,"count":3}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "format_error"},
		{"schema fence", "schema_only", "```json\n{\"answer\":{\"token\":\"N1234\",\"enabled\":true,\"count\":3}}\n```", map[string]any{"token": "N1234", "enabled": true, "count": 3}, "format_error"},
		{"schema extra answer field", "schema_only", `{"answer":{"token":"N1234","enabled":true,"count":3,"extra":1}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "incorrect"},
		{"schema wrong value", "schema_only", `{"answer":{"token":"N1234","enabled":false,"count":3}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "incorrect"},
		{"schema missing value", "schema_only", `{"answer":{"token":"N1234","enabled":true}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "incorrect"},
		{"schema duplicate key", "schema_only", `{"answer":{"token":"N1234","enabled":false,"enabled":true,"count":3}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "format_error"},
		{"schema whitespace in token", "schema_only", `{"answer":{"token":" N1234 ","enabled":true,"count":3}}`, map[string]any{"token": "N1234", "enabled": true, "count": 3}, "incorrect"},
		{"schema two responses", "schema_only", `{"answer":3} {"answer":3}`, 3, "format_error"},
		{"schema array structure", "schema_only", `{"answer":{"indices":[0.0,2e0]}}`, map[string]any{"indices": []int{0, 2}}, "correct"},
		{"schema wrong array value", "schema_only", `{"answer":{"indices":[0,1]}}`, map[string]any{"indices": []int{0, 2}}, "incorrect"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := ModelBenchmarkItem{ID: tc.name, Category: "logic", Difficulty: 4, Checker: tc.checker, Expected: tc.expected}
			result := ModelBenchmarkScoreAnswer(item, tc.response)
			require.Equal(t, tc.status, result.Status, result.FailureReason)
			if tc.status == "partial" {
				require.Equal(t, 0.5, result.Score)
			}
		})
	}
}

type modelBenchmarkAuditRunRepo struct {
	ModelBenchmarkRepository
	created *ModelBenchmarkRun
	stop    error
}

func (r *modelBenchmarkAuditRunRepo) CreateRun(_ context.Context, run *ModelBenchmarkRun) (*ModelBenchmarkRun, error) {
	r.created = run
	return nil, r.stop
}

func TestModelBenchmarkRunRecordsExecutedVersion(t *testing.T) {
	stop := errors.New("stop after capturing run metadata")
	repo := &modelBenchmarkAuditRunRepo{stop: stop}
	accounts := modelBenchmarkAccountSource{account: &Account{ID: 9, Platform: PlatformOpenAI}}
	svc := NewModelBenchmarkService(repo, accounts, nil, &ModelBenchmarkExecutor{})
	plan := &ModelBenchmarkPlan{ID: 3, AccountID: 9, ModelID: "test-model", SuiteID: "hard-v1", SuiteVersion: "1.1.0", ScoringVersion: "1.0.0"}
	require.ErrorIs(t, svc.ExecutePlan(context.Background(), plan), stop)
	require.Equal(t, ModelBenchmarkSuiteID, repo.created.SuiteID)
	require.Equal(t, ModelBenchmarkSuiteVersion, repo.created.SuiteVersion)
	require.Equal(t, ModelBenchmarkScoringVer, repo.created.ScoringVersion)
	require.Equal(t, "1.1.0", plan.SuiteVersion, "execution must not rewrite historical plan metadata")
}

// Every possible n generated by interval-dp has a separately enumerated optimum.
func TestModelBenchmarkIntervalMergeKnownAnswers(t *testing.T) {
	for n, want := range map[int]int{6: 51, 7: 74, 8: 102, 9: 135} {
		values := make([]int, n)
		for i := range values {
			values[i] = i + 1
		}
		cost, first := modelBenchmarkOptimalAdjacentMerge(values)
		require.Equal(t, want, cost)
		require.Equal(t, 0, first)
	}
}

// Check the stated lower-bound invariant over all nondecreasing arrays of
// length 0..6 with elements in {-1,0,1}, including absent targets and duplicates.
func TestModelBenchmarkLowerBoundInvariantSemantics(t *testing.T) {
	var visit func([]int, int)
	visit = func(values []int, remaining int) {
		for target := -2; target <= 2; target++ {
			lo, hi := 0, len(values)
			check := func() {
				require.True(t, 0 <= lo && lo <= hi && hi <= len(values))
				for _, value := range values[:lo] {
					require.Less(t, value, target)
				}
				for _, value := range values[hi:] {
					require.GreaterOrEqual(t, value, target)
				}
			}
			check()
			for lo < hi {
				mid := lo + (hi-lo)/2
				if values[mid] < target {
					lo = mid + 1
				} else {
					hi = mid
				}
				check()
			}
			want := 0
			for want < len(values) && values[want] < target {
				want++
			}
			require.Equal(t, want, lo)
		}
		if remaining == 0 {
			return
		}
		for value := -1; value <= 1; value++ {
			if len(values) == 0 || value >= values[len(values)-1] {
				visit(append(values, value), remaining-1)
			}
		}
	}
	visit(nil, 6)
}

func TestModelBenchmarkNumericalStabilitySemantics(t *testing.T) {
	// A 256-bit reference computes the original real expression. It does not
	// use the rationalized formula that the question selects.
	for exponent := 16; exponent <= 20; exponent++ {
		x := math.Pow10(exponent)
		value := new(big.Float).SetPrec(256).SetFloat64(x)
		plusOne := new(big.Float).SetPrec(256).Add(value, new(big.Float).SetPrec(256).SetInt64(1))
		a := new(big.Float).SetPrec(256).Sqrt(plusOne)
		b := new(big.Float).SetPrec(256).Sqrt(value)
		reference, _ := new(big.Float).SetPrec(256).Sub(a, b).Float64()
		stable := 1 / (math.Sqrt(x+1) + math.Sqrt(x))
		direct := math.Sqrt(x+1) - math.Sqrt(x)
		require.Greater(t, reference, 0.0)
		require.InDelta(t, 1.0, stable/reference, 1e-14)
		require.Zero(t, direct)
	}
}

func TestModelBenchmarkIndexLexicographicOrder(t *testing.T) {
	require.True(t, modelBenchmarkIndicesLess([]int{0, 2}, []int{0, 10}))
	require.True(t, modelBenchmarkIndicesLess([]int{0, 2}, []int{0, 2, 3}))
	require.False(t, modelBenchmarkIndicesLess([]int{0, 2, 3}, []int{0, 2}))
	require.False(t, modelBenchmarkIndicesLess([]int{0, 2}, []int{0, 2}))
}
