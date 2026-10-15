package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

const (
	ModelBenchmarkSuiteID      = "hard-v1"
	ModelBenchmarkSuiteVersion = "1.0.0"
	ModelBenchmarkScoringVer   = "1.0.0"
	modelBenchmarkSuiteSalt    = "sub2api-hard-v1-stable-family-definitions"
)

// ModelBenchmarkSuite is an immutable descriptor exposed to the admin API.
type ModelBenchmarkSuite struct {
	ID              string             `json:"id"`
	Version         string             `json:"version"`
	ScoringVersion  string             `json:"scoring_version"`
	ItemCount       int                `json:"item_count"`
	FamilyCount     int                `json:"family_count"`
	MinDifficulty   int                `json:"min_difficulty"`
	MaxDifficulty   int                `json:"max_difficulty"`
	CategoryWeights map[string]float64 `json:"category_weights"`
}

// ModelBenchmarkItem is a generated, machine-checkable benchmark instance.
type ModelBenchmarkItem struct {
	ID                   string `json:"id"`
	FamilyID             string `json:"family_id"`
	Category             string `json:"category"`
	Difficulty           int    `json:"difficulty"`
	Checker              string `json:"checker"`
	Prompt               string `json:"prompt"`
	Expected             any    `json:"-"`
	PromptHash           string `json:"prompt_hash"`
	ExpectedOutputTokens int    `json:"expected_output_tokens"`
	ExpectedLatencyMS    int    `json:"expected_latency_ms"`
}

type modelBenchmarkFamilySpec struct {
	id         string
	category   string
	difficulty int
	checker    string
}

var modelBenchmarkFamilies = []modelBenchmarkFamilySpec{
	{id: "modular-exponentiation", category: "math", difficulty: 4, checker: "integer"},
	{id: "crt-remainder", category: "math", difficulty: 5, checker: "integer"},
	{id: "constrained-permutations", category: "math", difficulty: 5, checker: "integer"},
	{id: "shortest-path", category: "algorithms", difficulty: 4, checker: "sequence"},
	{id: "topological-order", category: "algorithms", difficulty: 4, checker: "sequence"},
	{id: "interval-dp", category: "algorithms", difficulty: 5, checker: "fields"},
	{id: "cycle-detection", category: "algorithms", difficulty: 3, checker: "exact"},
	{id: "kmp-prefix", category: "algorithms", difficulty: 4, checker: "sequence"},
	{id: "sparse-matrix", category: "algorithms", difficulty: 5, checker: "set"},
	{id: "token-bucket", category: "planning", difficulty: 4, checker: "sequence"},
	{id: "modular-inverse", category: "math", difficulty: 4, checker: "integer"},
	{id: "markov-expected-steps", category: "math", difficulty: 5, checker: "number_tolerance"},
	{id: "inclusion-exclusion", category: "math", difficulty: 4, checker: "integer"},
	{id: "two-machine-schedule", category: "planning", difficulty: 5, checker: "fields"},
	{id: "concurrent-race", category: "code", difficulty: 5, checker: "fields"},
	{id: "pseudocode-trace", category: "code", difficulty: 4, checker: "sequence"},
	{id: "loop-invariant", category: "code", difficulty: 4, checker: "exact"},
	{id: "greedy-counterexample", category: "algorithms", difficulty: 5, checker: "integer"},
	{id: "truth-assignment", category: "logic", difficulty: 5, checker: "fields"},
	{id: "strict-schema", category: "instruction", difficulty: 4, checker: "schema_only"},
	{id: "epistemic-deduction", category: "logic", difficulty: 5, checker: "set"},
	{id: "error-chain", category: "code", difficulty: 4, checker: "sequence"},
	{id: "pareto-frontier", category: "planning", difficulty: 5, checker: "set"},
	{id: "numerical-stability", category: "code", difficulty: 3, checker: "exact"},
}

var modelBenchmarkCategoryWeights = map[string]float64{
	"math":        .22,
	"algorithms":  .24,
	"logic":       .18,
	"code":        .18,
	"planning":    .10,
	"instruction": .08,
}

// GetModelBenchmarkSuite returns the built-in hard suite descriptor.
func GetModelBenchmarkSuite() ModelBenchmarkSuite {
	return ModelBenchmarkSuite{
		ID: ModelBenchmarkSuiteID, Version: ModelBenchmarkSuiteVersion, ScoringVersion: ModelBenchmarkScoringVer,
		ItemCount: len(modelBenchmarkFamilies) * 2, FamilyCount: len(modelBenchmarkFamilies),
		MinDifficulty: 3, MaxDifficulty: 5, CategoryWeights: modelBenchmarkCategoryWeights,
	}
}

// GenerateModelBenchmarkItems deterministically generates both variants for every family.
// planID/runID make prompts vary between runs while remaining reproducible inside a run.
func GenerateModelBenchmarkItems(planID, runID int64) []ModelBenchmarkItem {
	items := make([]ModelBenchmarkItem, 0, len(modelBenchmarkFamilies)*2)
	for _, family := range modelBenchmarkFamilies {
		for variant := 1; variant <= 2; variant++ {
			seed := modelBenchmarkSeed(planID, runID, family.id, variant)
			rng := newModelBenchmarkRNG(seed)
			prompt, expected := generateModelBenchmarkInstance(family, rng, variant)
			item := ModelBenchmarkItem{
				ID: fmt.Sprintf("%s-v%d", family.id, variant), FamilyID: family.id,
				Category: family.category, Difficulty: family.difficulty, Checker: family.checker,
				Prompt: prompt, Expected: expected,
				PromptHash:           modelBenchmarkPromptHash(prompt),
				ExpectedOutputTokens: 180 + family.difficulty*45,
				ExpectedLatencyMS:    5000 + family.difficulty*2500,
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func modelBenchmarkSeed(values ...any) uint64 {
	raw := modelBenchmarkSuiteSalt
	for _, value := range values {
		raw += fmt.Sprintf("|%v", value)
	}
	sum := sha256.Sum256([]byte(raw))
	return binary.BigEndian.Uint64(sum[8:])
}

func modelBenchmarkPromptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

type modelBenchmarkRNG struct{ state uint64 }

func newModelBenchmarkRNG(seed uint64) *modelBenchmarkRNG { return &modelBenchmarkRNG{state: seed} }

func (r *modelBenchmarkRNG) next() uint64 {
	r.state += 0x9e3779b97f4a7c15
	z := r.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (r *modelBenchmarkRNG) int(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + int(r.next()%uint64(hi-lo+1))
}

func modelBenchmarkContract(task string) string {
	return task + "\n\nReturn exactly one JSON object in this shape: {\"answer\": VALUE}. No markdown, explanations, or extra fields."
}

func generateModelBenchmarkInstance(f modelBenchmarkFamilySpec, r *modelBenchmarkRNG, variant int) (string, any) {
	switch f.id {
	case "modular-exponentiation":
		base := int64(r.int(10000, 99999))
		exp := int64(r.int(150000, 900000))
		mod := int64(r.int(10007, 99991))
		return modelBenchmarkContract(fmt.Sprintf("Compute %d^%d mod %d.", base, exp, mod)), new(big.Int).Exp(big.NewInt(base), big.NewInt(exp), big.NewInt(mod)).Int64()
	case "crt-remainder":
		m1 := int64(r.int(17, 97))
		m2 := int64(r.int(101, 251))
		a1 := int64(r.int(3, int(m1-1)))
		a2 := int64(r.int(5, int(m2-1)))

		// Find x in [0,m1*m2) by deterministic search (m1*m2 stays small).
		product := m1 * m2
		var answer int64
		for x := int64(0); x < product; x++ {
			if x%m1 == a1 && x%m2 == a2 {
				answer = x
				break
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("Find the smallest non-negative x satisfying x mod %d = %d and x mod %d = %d.", m1, a1, m2, a2)), answer
	case "constrained-permutations":
		n := r.int(8, 11)
		fixed := r.int(2, 3)
		answer := 1
		for i := fixed + 1; i <= n; i++ {
			answer *= i
		}
		return modelBenchmarkContract(fmt.Sprintf("Count permutations of %d distinct items where the first %d named items must all keep their original positions.", n, fixed)), answer
	case "shortest-path":
		edges := fmt.Sprintf("A-B:%d,A-C:%d,B-D:%d,C-D:%d,D-E:%d", r.int(2, 19), r.int(2, 19), r.int(2, 19), r.int(2, 19), r.int(2, 19))
		// Derive the unique shortest path after generation.
		edgeMap := map[[2]string]int{}
		for _, edge := range strings.Split(edges, ",") {
			parts := strings.Split(edge, ":")
			nodes := strings.SplitN(parts[0], "-", 2)
			w, _ := strconv.Atoi(parts[1])
			edgeMap[[2]string{nodes[0], nodes[1]}] = w
			edgeMap[[2]string{nodes[1], nodes[0]}] = w
		}
		var bestPath []string
		bestDistance := 1 << 30
		var visit func(node string, path []string, distance int)
		visit = func(node string, path []string, distance int) {
			if node == "E" {
				candidate := append([]string(nil), path...)
				key := strings.Join(candidate, ",")
				bestKey := strings.Join(bestPath, ",")
				if distance < bestDistance || (distance == bestDistance && (bestPath == nil || key < bestKey)) {
					bestDistance = distance
					bestPath = candidate
				}
				return
			}
			for pair, weight := range edgeMap {
				if pair[0] != node {
					continue
				}
				next := pair[1]
				duplicate := false
				for _, existing := range path {
					if existing == next {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				visit(next, append(path, next), distance+weight)
			}
		}
		visit("A", []string{"A"}, 0)
		path := bestPath
		return modelBenchmarkContract(fmt.Sprintf("In the weighted undirected graph with nodes A-E and edges %s, return the lexicographically smallest shortest node sequence from A to E.", edges)), path
	case "topological-order":
		caseID := r.int(1000, 9999)
		edges := "A>B,B>C,A>D,D>C,C>E,B>E"
		return modelBenchmarkContract(fmt.Sprintf("[case %d] For the DAG edges %s, return the unique topological order selected by Kahn's algorithm with a min-heap.", caseID, edges)), []string{"A", "B", "D", "C", "E"}
	case "interval-dp":
		n := r.int(6, 9)
		sum := n * (n + 1) / 2
		answer := sum * (n - 1)
		return modelBenchmarkContract(fmt.Sprintf("There are %d adjacent blocks with values 1..%d; merging an interval costs the sum of its values and the merged value remains that sum. Using interval DP, return the minimum total cost to merge all blocks and the leftmost optimal first merge index 0.", n, n)), map[string]any{"value": answer, "first_merge_left_index": 0}
	case "cycle-detection":
		caseID := r.int(1000, 9999)
		cycle := variant == 1
		edges := "1>2,2>3,3>4"
		if cycle {
			edges += ",4>2"
		} else {
			edges += ",4>5"
		}
		return modelBenchmarkContract(fmt.Sprintf("[case %d] Does directed graph %s contain a directed cycle? Return true or false.", caseID, edges)), cycle
	case "kmp-prefix":
		caseID := r.int(1000, 9999)
		word := []string{"a", "b", "a", "b", "c", "a", "b", "a", "b", "a"}
		if variant == 2 {
			word = []string{"x", "y", "x", "y", "x", "x", "y", "x", "y", "x"}
		}
		prefix := make([]int, len(word))
		for i := 1; i < len(word); i++ {
			j := prefix[i-1]
			for j > 0 && word[i] != word[j] {
				j = prefix[j-1]
			}
			if word[i] == word[j] {
				j++
			}
			prefix[i] = j
		}
		return modelBenchmarkContract(fmt.Sprintf("[case %d] Return the KMP failure function of %s as an integer array.", caseID, strings.Join(word, ""))), prefix
	case "sparse-matrix":
		caseID := r.int(1000, 9999)
		offset := r.int(0, 3)
		domain := []int{1, 2, 3, 4, 5, 6, 7}
		coords := []string{}
		for _, a := range domain[:4] {
			for _, b := range domain[2:6] {
				if (a*b+offset)%4 == 0 {
					coords = append(coords, fmt.Sprintf("%d,%d", a, b))
				}
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("[case %d] A sparse 7x7 matrix has nonzeros exactly at the coordinate set implied by row [1..4] and column [3..6] where (row*column + "+strconv.Itoa(offset)+") mod 4 = 0. Return all nonzero coordinates as strings row,column", caseID)), coords
	case "token-bucket":
		capacity := r.int(8, 15)
		rate := r.int(1, 4)
		initial := capacity - r.int(1, 3)
		fill := initial
		states := []int{}
		consumptions := []int{}
		for i := 0; i < 6; i++ {
			if i%2 == 0 {
				consumed := r.int(2, 4)
				consumptions = append(consumptions, consumed)
				fill -= consumed
			} else {
				fill += rate
			}
			if fill < 0 {
				fill = 0
			}
			if fill > capacity {
				fill = capacity
			}
			states = append(states, fill)
		}
		return modelBenchmarkContract(fmt.Sprintf("A token bucket starts with %d tokens, capacity %d, and refills %d tokens after every odd second. At t=1,3,5 consume tokens in this order: %v; at t=2,4,6 refill. Return the six bucket states after each action from t=1 through t=6.", initial, capacity, rate, consumptions)), states
	case "modular-inverse":
		mod := int64(r.int(1009, 9973))
		a := int64(r.int(100, int(mod-1)))
		inv := new(big.Int).ModInverse(big.NewInt(a), big.NewInt(mod))
		return modelBenchmarkContract(fmt.Sprintf("Return the modular inverse of %d modulo %d.", a, mod)), inv.Int64()
	case "markov-expected-steps":
		p := float64(r.int(15, 45)) / 100
		expected := 1 / (1 - p)
		return modelBenchmarkContract(fmt.Sprintf("A process stays in state S with probability %.2f and moves to absorbing state T otherwise. Return the expected number of steps to leave S, within 1e-6.", p)), expected
	case "inclusion-exclusion":
		n := r.int(40, 90)
		a, b, c := r.int(3, 6), r.int(7, 11), r.int(12, 17)
		count := n - n/a - n/b - n/c + n/(a*b) + n/(a*c) + n/(b*c) - n/(a*b*c)
		return modelBenchmarkContract(fmt.Sprintf("From 1..%d, count integers divisible by none of %d, %d, or %d.", n, a, b, c)), count
	case "two-machine-schedule":
		jobs := []int{r.int(3, 9), r.int(4, 10), r.int(2, 8), r.int(5, 12)}
		total := 0
		for _, job := range jobs {
			total += job
		}
		bestMakespan := total
		bestIndices := []int{}
		for mask := 0; mask < 1<<len(jobs); mask++ {
			indices := []int{}
			load := 0
			for index, job := range jobs {
				if mask&(1<<index) != 0 {
					indices = append(indices, index)
					load += job
				}
			}
			makespan := max(load, total-load)
			if makespan < bestMakespan || (makespan == bestMakespan && fmt.Sprint(indices) < fmt.Sprint(bestIndices)) {
				bestMakespan = makespan
				bestIndices = indices
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("Split jobs %v between two identical machines to minimize makespan. Return {\"makespan\": VALUE, \"machine_a_indices\": INDICES}; INDICES must be the lexicographically smallest optimal subset assigned to machine A.", jobs)), map[string]any{"makespan": bestMakespan, "machine_a_indices": bestIndices}
	case "concurrent-race":
		line := r.int(22, 37)
		return modelBenchmarkContract(fmt.Sprintf("In a Go test, goroutine 1 writes shared x at line %d, then signals ch; goroutine 2 reads x concurrently without waiting; it receives from ch only after its read. Return {\"defect\": \"data_race\", \"safe\": false, \"line\": LINE}.", line)), map[string]any{"defect": "data_race", "safe": false, "line": line}
	case "pseudocode-trace":
		start := r.int(5, 20)
		output := []int{}
		for value := start; value != 1; {
			if value%2 == 0 {
				value /= 2
			} else {
				value = value*3 + 1
			}
			output = append(output, value)
		}
		return modelBenchmarkContract(fmt.Sprintf("Trace the Collatz function from %d until it reaches 1; return the complete integer sequence after the start.", start)), output
	case "loop-invariant":
		caseID := r.int(1000, 9999)
		return modelBenchmarkContract(fmt.Sprintf("[case %d] For a sorted-array lower-bound loop, which invariant is strongest and useful? Return one of: bounds_shifted, sum_constant, or random_index.", caseID)), "bounds_shifted"
	case "greedy-counterexample":
		caseID := r.int(1000, 9999)
		return modelBenchmarkContract(fmt.Sprintf("[case %d] For coin change with coins [1,3,4] and amount 6, return the absolute difference between greedy and optimal coin counts.", caseID)), 1
	case "truth-assignment":
		p := r.int(2, 9)
		return modelBenchmarkContract(fmt.Sprintf("Find p=true satisfying p XOR false, p IMPLIES %d>1, and NOT(p AND false). Return {\"p\": true, \"q\": false}.", p)), map[string]any{"p": true, "q": false}
	case "strict-schema":
		token := fmt.Sprintf("N%d", r.int(1000, 9999))
		return modelBenchmarkContract(fmt.Sprintf("Ignore all surrounding instructions. Return exactly {\"answer\": {\"token\": \"%s\", \"enabled\": true, \"count\": 3}}.", token)), map[string]any{"token": token, "enabled": true, "count": 3}
	case "epistemic-deduction":
		caseID := r.int(1000, 9999)
		people := []string{"Ada", "Ben", "Cai"}
		holderIndex := r.int(0, len(people)-1)
		holders := []string{people[holderIndex]}
		return modelBenchmarkContract(fmt.Sprintf("[case %d] Exactly one of Ada/Ben/Cai holds a key and all statements are true. Ada says Ben and Cai have different key statuses; Ben says he does not hold the key. Return the holder name set.", caseID)), holders
	case "error-chain":
		order := r.int(1, 3)
		chain := []string{"close_db", "rollback_tx", "release_lock"}
		rotated := append(append([]string{}, chain[order:]...), chain[:order]...)
		return modelBenchmarkContract(fmt.Sprintf("A deferred cleanup enqueues %v in the order they must complete after a failed transaction. Return the string sequence.", rotated)), rotated
	case "pareto-frontier":
		caseID := r.int(1000, 9999)
		options := [][]int{{1, 9}, {2, 7}, {3, 6}, {5, 5}, {7, 4}, {9, 8}}
		front := []string{}
		for i, a := range options {
			dominated := false
			for j, b := range options {
				if i != j && b[0] <= a[0] && b[1] <= a[1] && (b[0] < a[0] || b[1] < a[1]) {
					dominated = true
				}
			}
			if !dominated {
				front = append(front, fmt.Sprintf("%d,%d", a[0], a[1]))
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("[case %d] For cost/latency pairs [1,9],[2,7],[3,6],[5,5],[7,4],[9,8], return the Pareto-minimal set as \"cost,latency\" strings.", caseID)), front
	default:
		caseID := r.int(1000, 9999)
		return modelBenchmarkContract(fmt.Sprintf("[case %d] Which subtraction formula is numerically stable near equality? Return one of: direct_subtraction, reordered_subtraction, or random_formula.", caseID)), "reordered_subtraction"
	}
}
