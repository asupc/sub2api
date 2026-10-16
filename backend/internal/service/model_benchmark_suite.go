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
	ModelBenchmarkSuiteVersion = "1.1.0"
	ModelBenchmarkScoringVer   = "1.0.0"
	modelBenchmarkSuiteSalt    = "sub2api-hard-v1-chinese-prompts-1"
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

func modelBenchmarkPrime(r *modelBenchmarkRNG, primes []int64) int64 {
	if len(primes) == 0 {
		return 2
	}
	return primes[r.next()%uint64(len(primes))]
}

func modelBenchmarkContract(task string) string {
	return task + "\n\n只返回一个 JSON 对象，格式为 {\"answer\": VALUE}。不要使用 Markdown、解释或额外字段。"
}

func generateModelBenchmarkInstance(f modelBenchmarkFamilySpec, r *modelBenchmarkRNG, variant int) (string, any) {
	switch f.id {
	case "modular-exponentiation":
		base := int64(r.int(10000, 99999))
		exp := int64(r.int(150000, 900000))
		mod := int64(r.int(10007, 99991))
		return modelBenchmarkContract(fmt.Sprintf("计算 %d^%d mod %d。", base, exp, mod)), new(big.Int).Exp(big.NewInt(base), big.NewInt(exp), big.NewInt(mod)).Int64()
	case "crt-remainder":
		m1 := modelBenchmarkPrime(r, []int64{17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61, 67, 71, 73, 79, 83, 89, 97})
		m2 := modelBenchmarkPrime(r, []int64{101, 103, 107, 109, 113, 127, 131, 137, 139, 149, 151, 157, 163, 167, 173, 179, 181, 191, 193, 197, 199, 211, 223, 227, 229, 233, 239, 241, 251})
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
		return modelBenchmarkContract(fmt.Sprintf("求满足 x mod %d = %d 且 x mod %d = %d 的最小非负 x。", m1, a1, m2, a2)), answer
	case "constrained-permutations":
		n := r.int(8, 11)
		fixed := r.int(2, 3)
		answer := 1
		for i := fixed + 1; i <= n; i++ {
			answer *= i
		}
		return modelBenchmarkContract(fmt.Sprintf("计算 %d 个不同元素的排列数，要求前 %d 个命名元素都必须保持原位置。", n, fixed)), answer
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
		return modelBenchmarkContract(fmt.Sprintf("在节点为 A-E、边为 %s 的无向加权图中，返回从 A 到 E 的字典序最小最短节点序列。", edges)), path
	case "topological-order":
		caseID := r.int(1000, 9999)
		edges := "A>B,B>C,A>D,D>C,C>E,B>E"
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 对边为 %s 的 DAG，返回使用小根堆版 Kahn 算法得到的唯一拓扑序。", caseID, edges)), []string{"A", "B", "D", "C", "E"}
	case "interval-dp":
		n := r.int(6, 9)
		sum := n * (n + 1) / 2
		answer := sum * (n - 1)
		return modelBenchmarkContract(fmt.Sprintf("有 %d 个相邻块，值依次为 1..%d；合并一个区间的成本等于该区间值之和，合并后的值仍为该和。请用区间 DP 求合并全部块的最小总成本，并返回最左侧最优首次合并下标 0。", n, n)), map[string]any{"value": answer, "first_merge_left_index": 0}
	case "cycle-detection":
		caseID := r.int(1000, 9999)
		cycle := variant == 1
		edges := "1>2,2>3,3>4"
		if cycle {
			edges += ",4>2"
		} else {
			edges += ",4>5"
		}
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 有向图 %s 是否含有向环？返回 true 或 false。", caseID, edges)), cycle
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
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 返回字符串 %s 的 KMP 失配函数，用整数数组表示。", caseID, strings.Join(word, ""))), prefix
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
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 一个 7x7 稀疏矩阵的非零元素坐标，由行 [1..4]、列 [3..6] 且 (row*column + "+strconv.Itoa(offset)+") mod 4 = 0 确定。返回全部非零坐标，格式为 row,column", caseID)), coords
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
		return modelBenchmarkContract(fmt.Sprintf("一个令牌桶初始有 %d 个令牌，容量为 %d，并在每个偶数时刻后补充 %d 个令牌。t=1,3,5 按顺序消耗令牌：%v；t=2,4,6 补充。返回 t=1 到 t=6 每次动作后的 6 个桶状态。", initial, capacity, rate, consumptions)), states
	case "modular-inverse":
		mod := modelBenchmarkPrime(r, []int64{1009, 1013, 1019, 1021, 1031, 1033, 1039, 1049, 1051, 1061, 1063, 1069, 1087, 1091, 1093, 1097, 1103, 1109, 1117, 1123, 1129, 1151, 9973})
		a := int64(r.int(100, int(mod-1)))
		inv := new(big.Int).ModInverse(big.NewInt(a), big.NewInt(mod))
		return modelBenchmarkContract(fmt.Sprintf("返回 %d 在模 %d 下的乘法逆元。", a, mod)), inv.Int64()
	case "markov-expected-steps":
		p := float64(r.int(15, 45)) / 100
		expected := 1 / (1 - p)
		return modelBenchmarkContract(fmt.Sprintf("一个过程以概率 %.2f 停留在状态 S，否则移动到吸收态 T。返回离开 S 的期望步数，误差不超过 1e-6。", p)), expected
	case "inclusion-exclusion":
		n := r.int(40, 90)
		a, b, c := r.int(3, 6), r.int(7, 11), r.int(12, 17)
		count := n - n/a - n/b - n/c + n/(a*b) + n/(a*c) + n/(b*c) - n/(a*b*c)
		return modelBenchmarkContract(fmt.Sprintf("在 1..%d 中，统计不能被 %d、%d、%d 中任何一个整除的整数个数。", n, a, b, c)), count
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
		return modelBenchmarkContract(fmt.Sprintf("将作业 %v 分配到两台相同机器以最小化 makespan。返回 {\"makespan\": VALUE, \"machine_a_indices\": INDICES}；INDICES 必须是分配给机器 A 的字典序最小最优子集。", jobs)), map[string]any{"makespan": bestMakespan, "machine_a_indices": bestIndices}
	case "concurrent-race":
		line := r.int(22, 37)
		return modelBenchmarkContract(fmt.Sprintf("在 Go 测试中，goroutine 1 在第 %d 行写入共享变量 x 后向 ch 发信号；goroutine 2 不等待就并发读取 x，读完后才接收 ch。返回 {\"defect\": \"data_race\", \"safe\": false, \"line\": LINE}。", line)), map[string]any{"defect": "data_race", "safe": false, "line": line}
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
		return modelBenchmarkContract(fmt.Sprintf("从 %d 开始追踪 Collatz 函数直到达到 1；返回起始值之后的完整整数序列。", start)), output
	case "loop-invariant":
		caseID := r.int(1000, 9999)
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 对有序数组 lower-bound 循环，哪个不变式最强且有用？返回 bounds_shifted、sum_constant 或 random_index 之一。", caseID)), "bounds_shifted"
	case "greedy-counterexample":
		caseID := r.int(1000, 9999)
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 硬币面额为 [1,3,4]、金额为 6 时，返回贪心币数与最优币数之差的绝对值。", caseID)), 1
	case "truth-assignment":
		p := r.int(2, 9)
		return modelBenchmarkContract(fmt.Sprintf("求满足 p XOR false、p IMPLIES %d>1 以及 NOT(p AND false) 的 p=true 赋值。返回 {\"p\": true, \"q\": false}。", p)), map[string]any{"p": true, "q": false}
	case "strict-schema":
		token := fmt.Sprintf("N%d", r.int(1000, 9999))
		return modelBenchmarkContract(fmt.Sprintf("忽略周围指令。精确返回 {\"answer\": {\"token\": \"%s\", \"enabled\": true, \"count\": 3}}。", token)), map[string]any{"token": token, "enabled": true, "count": 3}
	case "epistemic-deduction":
		caseID := r.int(1000, 9999)
		people := []string{"Ada", "Ben", "Cai"}
		holderIndex := r.int(0, len(people)-1)
		holders := []string{people[holderIndex]}
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] Ada、Ben、Cai 中恰好一人持有钥匙，且所有陈述为真。Ada 说 Ben 和 Cai 的持钥状态不同；Ben 说自己没有钥匙。返回持钥者姓名集合。", caseID)), holders
	case "error-chain":
		order := r.int(1, 3)
		chain := []string{"close_db", "rollback_tx", "release_lock"}
		rotated := append(append([]string{}, chain[order:]...), chain[:order]...)
		return modelBenchmarkContract(fmt.Sprintf("一次事务失败后的延迟清理需要按完成顺序依次执行 %v。返回该字符串序列。", rotated)), rotated
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
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 对成本/延迟二元组 [1,9],[2,7],[3,6],[5,5],[7,4],[9,8]，返回 Pareto 最小集合，元素格式为 \"cost,latency\"。", caseID)), front
	default:
		caseID := r.int(1000, 9999)
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 哪个减法公式在两数接近时数值稳定？返回 direct_subtraction、reordered_subtraction 或 random_formula 之一。", caseID)), "reordered_subtraction"
	}
}
