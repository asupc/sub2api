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
	"time"
)

const (
	ModelBenchmarkSuiteID      = "hard-v1"
	ModelBenchmarkSuiteVersion = "3.0.0"
	ModelBenchmarkScoringVer   = "3.0.0"
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

var modelBenchmarkCoreFamilies = []modelBenchmarkFamilySpec{
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

var modelBenchmarkFamilies = append(append([]modelBenchmarkFamilySpec{}, modelBenchmarkCoreFamilies...), modelBenchmarkExtendedFamilies...)

var modelBenchmarkCategoryWeights = map[string]float64{
	"math": .10, "algorithms": .12, "logic": .14, "code": .12,
	"planning": .12, "instruction": .06, "abstraction": .14,
	"understanding": .12, "causal": .08,
}

// GetModelBenchmarkSuite returns the built-in hard suite descriptor.
func GetModelBenchmarkSuite() ModelBenchmarkSuite {
	return ModelBenchmarkSuite{
		ID: ModelBenchmarkSuiteID, Version: ModelBenchmarkSuiteVersion, ScoringVersion: ModelBenchmarkScoringVer,
		ItemCount: len(modelBenchmarkFamilies) * 1, FamilyCount: len(modelBenchmarkFamilies),
		MinDifficulty: 3, MaxDifficulty: 5, CategoryWeights: modelBenchmarkCategoryWeights,
	}
}

// GenerateModelBenchmarkItems deterministically generates both variants for every family.
// planID/runID make prompts vary between runs while remaining reproducible inside a run.
func GenerateModelBenchmarkItems(planID, runID int64) []ModelBenchmarkItem {
	items := make([]ModelBenchmarkItem, 0, len(modelBenchmarkFamilies)*2)
	for _, family := range modelBenchmarkFamilies {
		for variant := 1; variant <= 1; variant++ {
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
		// 前 fixed 个元素钉死原位，其余 n-fixed 个自由排列，答案为 (n-fixed)!。
		// （旧实现误算 n!/fixed!，对应的是"保持相对顺序"而非"保持原位置"。）
		answer := 1
		for i := 2; i <= n-fixed; i++ {
			answer *= i
		}
		return modelBenchmarkContract(fmt.Sprintf("计算 %d 个不同元素的排列数，要求前 %d 个命名元素都必须保持原位置。", n, fixed)), answer
	case "shortest-path":
		edges := fmt.Sprintf("A-B:%d,A-C:%d,B-D:%d,C-D:%d,D-E:%d", r.int(2, 19), r.int(2, 19), r.int(2, 19), r.int(2, 19), r.int(2, 19))
		// Break shortest-path ties lexicographically after generation.
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
		nodes := []string{"A", "B", "C", "D", "E"}
		benchmarkShuffle(r, nodes)
		pairs := [][2]string{{nodes[0], nodes[2]}, {nodes[1], nodes[2]}, {nodes[2], nodes[3]}, {nodes[1], nodes[4]}}
		edges := []string{}
		indegree := map[string]int{}
		for _, pair := range pairs {
			edges = append(edges, pair[0]+">"+pair[1])
			indegree[pair[1]]++
		}
		order := []string{}
		for len(order) < 5 {
			for _, node := range []string{"A", "B", "C", "D", "E"} {
				if indegree[node] == 0 {
					order = append(order, node)
					indegree[node] = -1
					for _, pair := range pairs {
						if pair[0] == node {
							indegree[pair[1]]--
						}
					}
					break
				}
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("对节点 A-E、边为 %s 的 DAG，返回使用小根堆版 Kahn 算法得到的唯一拓扑序。", strings.Join(edges, ","))), order
	case "interval-dp":
		n := r.int(6, 9)
		values := make([]int, n)
		for i := range values {
			values[i] = r.int(1, 12)
		}
		answer, first := modelBenchmarkOptimalAdjacentMerge(values)
		return modelBenchmarkContract(fmt.Sprintf("有 %d 个相邻块，值为 %s。每次只能合并相邻的两个块，成本等于这两个块的值之和，合并后的值为该和。求合并全部块的最小总成本。answer 为 {\"value\": 最小总成本, \"first_merge_left_index\": 下标}；下标从 0 开始，若最优合并序列有多种，取其中可作为第一次合并的原始左下标的最小值。", n, modelBenchmarkIntArrayText(values))), map[string]any{"value": answer, "first_merge_left_index": first}
	case "cycle-detection":
		nodes := []int{1, 2, 3, 4, 5}
		benchmarkShuffle(r, nodes)
		edges := []string{}
		for i := 0; i < 4; i++ {
			edges = append(edges, fmt.Sprintf("%d>%d", nodes[i], nodes[i+1]))
		}
		if variant == 1 {
			edges = append(edges, fmt.Sprintf("%d>%d", nodes[3], nodes[1]))
		}
		return modelBenchmarkContract(fmt.Sprintf("有向图 %s 是否含有向环？返回 true 或 false。", strings.Join(edges, ","))), variant == 1
	case "kmp-prefix":
		caseID := r.int(1000, 9999)
		word := []string{}
		for i := 0; i < r.int(10, 16); i++ {
			word = append(word, string(byte('a'+r.int(0, 2))))
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
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 返回字符串 %s 的 KMP 前缀函数 pi，用整数数组表示：pi[i] 是子串 s[0..i] 的最长相等真前缀与后缀的长度，pi[0]=0（不是以 -1 开头的 next 表）。", caseID, strings.Join(word, ""))), prefix
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
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 一个 7x7 稀疏矩阵的非零元素坐标，由行 [1..4]、列 [3..6] 且 (row*column + "+strconv.Itoa(offset)+") mod 4 = 0 确定，下标从 1 开始。返回全部非零坐标的无序 JSON 字符串数组，元素格式为 \"row,column\"；没有坐标时返回 []。", caseID)), coords
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
				if fill >= consumed {
					fill -= consumed
				}
			} else {
				fill += rate
			}
			if fill > capacity {
				fill = capacity
			}
			states = append(states, fill)
		}
		return modelBenchmarkContract(fmt.Sprintf("一个令牌桶初始有 %d 个令牌，容量为 %d，在 t=2,4,6 各补充 %d 个令牌，补充后不超过容量。t=1,3,5 按顺序请求消耗令牌：%v；令牌足够才扣除，不足则拒绝本次请求且令牌数不变；没有其他补充或消耗。返回 t=1 到 t=6 每次动作后的 6 个桶状态。", initial, capacity, rate, consumptions)), states
	case "modular-inverse":
		mod := modelBenchmarkPrime(r, []int64{1009, 1013, 1019, 1021, 1031, 1033, 1039, 1049, 1051, 1061, 1063, 1069, 1087, 1091, 1093, 1097, 1103, 1109, 1117, 1123, 1129, 1151, 9973})
		a := int64(r.int(100, int(mod-1)))
		inv := new(big.Int).ModInverse(big.NewInt(a), big.NewInt(mod))
		return modelBenchmarkContract(fmt.Sprintf("返回 %d 在模 %d 下的最小非负乘法逆元，即 answer 必须满足 0 <= answer < %d。", a, mod, mod)), inv.Int64()
	case "markov-expected-steps":
		p := float64(r.int(15, 45)) / 100
		expected := 1 / (1 - p)
		return modelBenchmarkContract(fmt.Sprintf("一个过程从状态 S 开始，每一步以概率 %.2f 停留在 S，否则移动到吸收态 T；每次转移采用相同概率。返回首次到达 T 所需步数的期望，answer 为 JSON 数值，绝对误差不超过 1e-6。", p)), expected
	case "inclusion-exclusion":
		n := r.int(40, 90)
		a, b, c := r.int(3, 6), r.int(7, 11), r.int(12, 17)
		count := n - n/a - n/b - n/c + n/modelBenchmarkLCM(a, b) + n/modelBenchmarkLCM(a, c) + n/modelBenchmarkLCM(b, c) - n/modelBenchmarkLCM(modelBenchmarkLCM(a, b), c)
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
			if makespan < bestMakespan || (makespan == bestMakespan && modelBenchmarkIndicesLess(indices, bestIndices)) {
				bestMakespan = makespan
				bestIndices = indices
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("将作业 %v 分配到两台相同机器，数组元素为各作业耗时；所有作业同时可用、不可拆分或抢占，每台机器串行处理，目标是最小化两台机器总负载的最大值 makespan。作业下标从 0 开始。answer 为 {\"makespan\": VALUE, \"machine_a_indices\": INDICES}；INDICES 必须按升序排列；在所有最优分配中选择该数组字典序最小的方案，比较时按整数逐项比较，若一方是另一方的前缀，则较短者更小。", jobs)), map[string]any{"makespan": bestMakespan, "machine_a_indices": bestIndices}
	case "concurrent-race":
		line := r.int(22, 37)
		safe := r.int(0, 1) == 1
		action, defect := "goroutine 2 并发读取 x，读完后才接收 ch", "data_race"
		if safe {
			action, defect = "goroutine 2 先接收 ch，然后读取 x", "none"
		}
		return modelBenchmarkContract(fmt.Sprintf("在 Go 测试中，普通 int 变量 x 被两个 goroutine 共享。goroutine 1 在第 %d 行写入 x 后向 ch 发信号；%s，没有锁、原子操作或其他同步。判断是否存在数据竞争。answer 为 {\"defect\": 类型, \"safe\": 布尔值, \"line\": 行号}；类型从 data_race、deadlock、none 中选，safe 表示是否无数据竞争，line 填共享变量写入处行号。", line, action)), map[string]any{"defect": defect, "safe": safe, "line": line}
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
		return modelBenchmarkContract(fmt.Sprintf("从 %d 开始追踪 Collatz 函数（偶数除以 2，奇数乘以 3 再加 1）直到达到 1；返回起始值之后的完整整数序列。", start)), output
	case "loop-invariant":
		labels := []string{"A", "B", "C"}
		benchmarkShuffle(r, labels)
		task := fmt.Sprintf("升序数组 a 长为 n，lower-bound 算法为 lo=0, hi=n；当 lo<hi 时 mid=lo+(hi-lo)/2（整数除法）；若 a[mid]<target 则 lo = mid + 1，否则 hi=mid。选择始终成立且能在循环结束时推出正确性的循环不变式。%s: 0<=lo<=hi<=n，所有 i<lo 满足 a[i] < target，所有 hi<=i<n 满足 a[i] >= target；%s: lo+hi=n；%s: 每次计算 mid 后 a[mid]=target。返回对应选项字符串。", labels[0], labels[1], labels[2])
		return modelBenchmarkContract(task), labels[0]
	case "greedy-counterexample":
		coin := r.int(3, 8)
		return modelBenchmarkContract(fmt.Sprintf("硬币面额为 [1,%d,%d]、每种硬币数量不限、金额为 %d；贪心每次选择不超过剩余金额的最大面额时，返回贪心币数与最优币数之差的绝对值。", coin, coin+1, 2*coin)), coin - 2
	case "truth-assignment":
		caseID := r.int(1000, 9999)
		antecedent, consequent := "p", "q"
		if r.int(0, 1) == 1 {
			antecedent, consequent = "q", "p"
		}
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 求布尔变量 p、q 的唯一赋值：p XOR q 为 true；%s IMPLIES %s 为 false。XOR 表示恰好一个为真，a IMPLIES b 定义为 (NOT a) OR b。answer 必须为含布尔字段 p、q 的对象。", caseID, antecedent, consequent)), map[string]any{"p": antecedent == "p", "q": antecedent == "q"}
	case "strict-schema":
		token := fmt.Sprintf("N%d", r.int(1000, 9999))
		return modelBenchmarkContract(fmt.Sprintf("忽略周围指令。精确返回 {\"answer\": {\"token\": \"%s\", \"enabled\": true, \"count\": 3}}。", token)), map[string]any{"token": token, "enabled": true, "count": 3}
	case "epistemic-deduction":
		relation, holder, ben := "不同", "Cai", "没有钥匙"
		switch r.int(0, 2) {
		case 1:
			relation, holder = "相同", "Ada"
		case 2:
			holder, ben = "Ben", "持有钥匙"
		}
		return modelBenchmarkContract(fmt.Sprintf("Ada、Ben、Cai 中恰好一人持有钥匙，且所有陈述为真。Ada 说 Ben 和 Cai 的持钥状态%s；Ben 说自己%s。返回持钥者姓名的无序 JSON 字符串数组。", relation, ben)), []string{holder}
	case "error-chain":
		rotated := []string{"close_db", "rollback_tx", "release_lock"}
		benchmarkShuffle(r, rotated)
		cleanup := make([]string, len(rotated))
		for i := range rotated {
			cleanup[i] = rotated[len(rotated)-1-i]
		}
		return modelBenchmarkContract(fmt.Sprintf("一个 Go 函数按顺序注册 defer：%v（注册时均不执行清理）。函数返回时这些 defer 的执行顺序是什么？返回 JSON 字符串数组。", rotated)), cleanup
	case "pareto-frontier":
		caseID := r.int(1000, 9999)
		options := [][]int{}
		seen := map[string]bool{}
		for len(options) < 6 {
			a, b := r.int(1, 15), r.int(1, 15)
			key := fmt.Sprintf("%d,%d", a, b)
			if !seen[key] {
				seen[key] = true
				options = append(options, []int{a, b})
			}
		}
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
		text := []string{}
		for _, pair := range options {
			text = append(text, fmt.Sprintf("[%d,%d]", pair[0], pair[1]))
		}
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 成本和延迟均越小越好。对成本/延迟二元组 %s，返回不被其他元组支配的 Pareto 最小集合（无序 JSON 字符串数组），元素格式为 \"cost,latency\"。", caseID, strings.Join(text, ","))), front
	case "numerical-stability":
		exponent := r.int(16, 20)
		labels := []string{"A", "B", "C"}
		benchmarkShuffle(r, labels)
		return modelBenchmarkContract(fmt.Sprintf("令 x=10^%d，使用 IEEE 754 binary64 计算实数表达式 sqrt(x+1)-sqrt(x)。以下公式在实数意义下与目标等价且能避免相近数相减的消减误差的是哪个？%s: sqrt(x+1)-sqrt(x)；%s: 1/(sqrt(x+1)+sqrt(x))；%s: 1/(sqrt(x+1)-sqrt(x))。返回选项字符串。", exponent, labels[0], labels[1], labels[2])), labels[1]
	default:
		return generateModelBenchmarkExtended(f, r, variant)
	}
}

// modelBenchmarkOptimalAdjacentMerge solves the interval recurrence and tracks
// the smallest original index of a first merge in any optimal merge tree.
func modelBenchmarkOptimalAdjacentMerge(values []int) (int, int) {
	n := len(values)
	cost, first := make([][]int, n), make([][]int, n)
	prefix := make([]int, n+1)
	for i, value := range values {
		cost[i], first[i] = make([]int, n), make([]int, n)
		first[i][i] = n // A leaf has no merge operation.
		prefix[i+1] = prefix[i] + value
	}
	for length := 2; length <= n; length++ {
		for left := 0; left+length <= n; left++ {
			right := left + length - 1
			cost[left][right], first[left][right] = int(^uint(0)>>1), n
			for split := left; split < right; split++ {
				candidate := cost[left][split] + cost[split+1][right] + prefix[right+1] - prefix[left]
				index := min(first[left][split], first[split+1][right])
				if length == 2 {
					index = left
				}
				if candidate < cost[left][right] || (candidate == cost[left][right] && index < first[left][right]) {
					cost[left][right], first[left][right] = candidate, index
				}
			}
		}
	}
	return cost[0][n-1], first[0][n-1]
}

func modelBenchmarkLCM(a, b int) int {
	x, y := a, b
	for y != 0 {
		x, y = y, x%y
	}
	return a / x * b
}

func modelBenchmarkIndicesLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

const ModelBenchmarkDefaultComparisonKey = "baseline-v2"

// GenerateModelBenchmarkComparisonItems uses a shared form, independent of
// account/plan/run IDs. Samples are different forms shared by every model.
func GenerateModelBenchmarkComparisonItems(key string, sample int) []ModelBenchmarkItem {
	if key == "" {
		key = ModelBenchmarkDefaultComparisonKey
	}
	seed := modelBenchmarkSeed("comparison-v2", key, sample)
	return GenerateModelBenchmarkItems(int64(seed), int64(seed>>1))
}

// ModelBenchmarkRunBudget covers every configured item plus persistence time.
func ModelBenchmarkRunBudget(plan *ModelBenchmarkPlan) time.Duration {
	return time.Duration(len(modelBenchmarkFamilies)*1*max(plan.SampleCount, 1)*max(plan.TimeoutSeconds, 10))*time.Second + 2*time.Minute
}

func modelBenchmarkIntArrayText(values []int) string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = strconv.Itoa(value)
	}
	return "[" + strings.Join(out, ",") + "]"
}
