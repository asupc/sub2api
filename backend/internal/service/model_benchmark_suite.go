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
	ModelBenchmarkSuiteVersion = "7.0.0"
	ModelBenchmarkScoringVer   = "7.0.0"
	modelBenchmarkSuiteSalt    = "sub2api-hard-v1-hardened-7"
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

var modelBenchmarkFamilies = func() []modelBenchmarkFamilySpec {
	families := append([]modelBenchmarkFamilySpec{}, modelBenchmarkCoreFamilies...)
	families = append(families, modelBenchmarkExtendedFamilies...)
	families = append(families, modelBenchmarkCodingFamilies...)
	families = append(families, modelBenchmarkD6Families...)
	return append(families, modelBenchmarkD7Families...)
}()

var modelBenchmarkCategoryWeights = map[string]float64{
	"math": .05, "algorithms": .18, "logic": .12, "code": .28,
	"planning": .08, "instruction": .06, "abstraction": .10,
	"understanding": .08, "causal": .05,
}

// GetModelBenchmarkSuite returns the built-in hard suite descriptor.
func GetModelBenchmarkSuite() ModelBenchmarkSuite {
	return ModelBenchmarkSuite{
		ID: ModelBenchmarkSuiteID, Version: ModelBenchmarkSuiteVersion, ScoringVersion: ModelBenchmarkScoringVer,
		ItemCount: len(modelBenchmarkFamilies) * 1, FamilyCount: len(modelBenchmarkFamilies),
		MinDifficulty: 3, MaxDifficulty: 7, CategoryWeights: modelBenchmarkCategoryWeights,
	}
}

// GenerateModelBenchmarkItems deterministically generates one item per family.
// planID/runID make prompts vary between runs while remaining reproducible inside a run.
func GenerateModelBenchmarkItems(planID, runID int64) []ModelBenchmarkItem {
	items := make([]ModelBenchmarkItem, 0, len(modelBenchmarkFamilies))
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
		// 模数放大到 1e9 量级后，64 位中间乘积仍小于 2^63，可直接用快速幂复算。
		base := int64(r.int(100000000, 999999999))
		exp := int64(r.int(1000000000, 4000000000))
		mod := int64(r.int(500000000, 999999999))
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
		n := r.int(12, 15)
		fixed := r.int(3, 5)
		// 前 fixed 个元素钉死原位，其余 n-fixed 个自由排列，答案为 (n-fixed)!。
		// （旧实现误算 n!/fixed!，对应的是"保持相对顺序"而非"保持原位置"。）
		answer := 1
		for i := 2; i <= n-fixed; i++ {
			answer *= i
		}
		return modelBenchmarkContract(fmt.Sprintf("计算 %d 个不同元素的排列数，要求前 %d 个命名元素都必须保持原位置。", n, fixed)), answer
	case "shortest-path":
		// 9 个节点、13 条边；最短路径可能经过任意中间节点，不再是 5 节点小图。
		labels := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
		edges := []string{}
		seen := map[[2]string]bool{}
		addEdge := func(a, b string, w int) {
			key := [2]string{a, b}
			if a > b {
				key = [2]string{b, a}
			}
			if a == b || seen[key] {
				return
			}
			seen[key] = true
			edges = append(edges, fmt.Sprintf("%s-%s:%d", a, b, w))
		}
		for i := 0; i+1 < len(labels); i++ {
			addEdge(labels[i], labels[i+1], r.int(2, 19))
		}
		for i := 0; i < 5; i++ {
			a, b := r.int(0, 8), r.int(0, 8)
			addEdge(labels[min(a, b)], labels[max(a, b)], r.int(2, 19))
		}
		edgeMap := map[[2]string]int{}
		adjacency := map[string][]string{}
		for _, edge := range edges {
			parts := strings.Split(edge, ":")
			nodes := strings.SplitN(parts[0], "-", 2)
			w, _ := strconv.Atoi(parts[1])
			edgeMap[[2]string{nodes[0], nodes[1]}] = w
			edgeMap[[2]string{nodes[1], nodes[0]}] = w
			adjacency[nodes[0]] = append(adjacency[nodes[0]], nodes[1])
			adjacency[nodes[1]] = append(adjacency[nodes[1]], nodes[0])
		}
		var bestPath []string
		bestDistance := 1 << 30
		var visit func(node string, path []string, distance int)
		visit = func(node string, path []string, distance int) {
			if distance > bestDistance {
				return
			}
			if node == "I" {
				candidate := append([]string(nil), path...)
				key := strings.Join(candidate, ",")
				bestKey := strings.Join(bestPath, ",")
				if distance < bestDistance || (distance == bestDistance && (bestPath == nil || key < bestKey)) {
					bestDistance = distance
					bestPath = candidate
				}
				return
			}
			for _, next := range adjacency[node] {
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
				visit(next, append(path, next), distance+edgeMap[[2]string{node, next}])
			}
		}
		visit("A", []string{"A"}, 0)
		path := bestPath
		return modelBenchmarkContract(fmt.Sprintf("在节点为 A-I、边为 %s 的无向加权图中，返回从 A 到 I 的字典序最小最短节点序列。", strings.Join(edges, ","))), path
	case "topological-order":
		// 10 个节点、带分叉的 DAG；小根堆 Kahn 的结果必须逐步推导，不能凭直觉排列。
		nodes := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J"}
		index := map[string]int{}
		for i, node := range nodes {
			index[node] = i
		}
		fixed := [][2]string{{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"}, {"D", "E"}, {"C", "F"}, {"F", "G"}, {"E", "G"}, {"G", "H"}, {"B", "I"}, {"I", "J"}, {"H", "J"}}
		seenPairs := map[[2]int]bool{}
		clean := [][2]string{}
		for _, pair := range fixed {
			key := [2]int{index[pair[0]], index[pair[1]]}
			if !seenPairs[key] {
				seenPairs[key] = true
				clean = append(clean, pair)
			}
		}
		for i := 0; i < 5; i++ {
			a, b := r.int(0, 8), r.int(1, 9)
			if a >= b || seenPairs[[2]int{a, b}] {
				continue
			}
			seenPairs[[2]int{a, b}] = true
			clean = append(clean, [2]string{nodes[a], nodes[b]})
		}
		edges := []string{}
		indegree := map[string]int{}
		for _, node := range nodes {
			indegree[node] = 0
		}
		for _, pair := range clean {
			edges = append(edges, pair[0]+">"+pair[1])
			indegree[pair[1]]++
		}
		order := []string{}
		for len(order) < len(nodes) {
			next := ""
			for _, node := range nodes {
				if indegree[node] == 0 {
					next = node
					break
				}
			}
			order = append(order, next)
			indegree[next] = -1
			for _, pair := range clean {
				if pair[0] == next {
					indegree[pair[1]]--
				}
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("对节点 A-J、边为 %s 的 DAG，返回使用小根堆版 Kahn 算法（每步选入度为 0 的最小字母节点）得到的唯一拓扑序。", strings.Join(edges, ","))), order
	case "interval-dp":
		n := r.int(11, 14)
		values := make([]int, n)
		for i := range values {
			values[i] = r.int(1, 12)
		}
		answer, first := modelBenchmarkOptimalAdjacentMerge(values)
		return modelBenchmarkContract(fmt.Sprintf("有 %d 个相邻块，值为 %s。每次只能合并相邻的两个块，成本等于这两个块的值之和，合并后的值为该和。求合并全部块的最小总成本。answer 为 {\"value\": 最小总成本, \"first_merge_left_index\": 下标}；下标从 0 开始，若最优合并序列有多种，取其中可作为第一次合并的原始左下标的最小值。", n, modelBenchmarkIntArrayText(values))), map[string]any{"value": answer, "first_merge_left_index": first}
	case "cycle-detection":
		// 返回环成员集合而不是布尔值：不允许 50% 盲猜，且环可能不在起始链上。
		nodes := []int{1, 2, 3, 4, 5, 6, 7, 8}
		benchmarkShuffle(r, nodes)
		edges := [][2]int{}
		for i := 0; i+1 < len(nodes); i++ {
			edges = append(edges, [2]int{nodes[i], nodes[i+1]})
		}
		if variant == 1 {
			// 尾节点回连到链中第 3 个节点，形成 5 个节点的环。
			edges = append(edges, [2]int{nodes[len(nodes)-1], nodes[2]})
		} else {
			// 无环，但多出的横边使度数高的节点看起来像汇合点。
			edges = append(edges, [2]int{nodes[1], nodes[4]}, [2]int{nodes[0], nodes[5]})
		}
		reach := benchmarkReachability(9, edges)
		members := []int{}
		for i := 1; i <= 8; i++ {
			if reach[i][i] {
				members = append(members, i)
			}
		}
		text := []string{}
		for _, edge := range edges {
			text = append(text, fmt.Sprintf("%d>%d", edge[0], edge[1]))
		}
		return modelBenchmarkContract(fmt.Sprintf("有向图边为 %s。返回属于至少一个有向环的全部节点编号（升序整数数组）；无环时返回 []。", strings.Join(text, ","))), members
	case "kmp-prefix":
		// 30 个字符、周期前缀更多，pi 不能靠局部观察得到。
		caseID := r.int(1000, 9999)
		word := []string{}
		for i := 0; i < r.int(28, 34); i++ {
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
		// 14x14 矩阵配两个模条件，需要同时满足行列两个约束才能枚举正确。
		caseID := r.int(1000, 9999)
		offset, offset2 := r.int(0, 3), r.int(0, 4)
		coords := []string{}
		for row := 1; row <= 14; row++ {
			for col := 1; col <= 14; col++ {
				if row >= 2 && row <= 12 && col >= 3 && col <= 13 && (row*col+offset)%4 == 0 && (row+col+offset2)%3 == 0 {
					coords = append(coords, fmt.Sprintf("%d,%d", row, col))
				}
			}
		}
		return modelBenchmarkContract(fmt.Sprintf("[案例 %d] 一个 14x14 稀疏矩阵的非零元素坐标，由行 [2..12]、列 [3..13]、且 (row*column + %d) mod 4 = 0 且 (row + column + %d) mod 3 = 0 同时成立确定，下标从 1 开始。返回全部非零坐标的无序 JSON 字符串数组，元素格式为 \"row,column\"；没有坐标时返回 []。", caseID, offset, offset2)), coords
	case "token-bucket":
		// 14 个动作、补充量可变，且某些请求会耗尽后再补充，需逐步追踪。
		capacity := r.int(12, 20)
		rate := r.int(1, 5)
		initial := capacity - r.int(1, 4)
		fill := initial
		states := []int{}
		consumptions := []int{}
		actions := []string{}
		for i := 0; i < 14; i++ {
			if i%2 == 0 {
				consumed := r.int(0, 6)
				consumptions = append(consumptions, consumed)
				actions = append(actions, fmt.Sprintf("t=%d 请求 %d", i+1, consumed))
				if fill >= consumed {
					fill -= consumed
				}
			} else {
				topUp := rate + r.int(0, 2)
				actions = append(actions, fmt.Sprintf("t=%d 补充 %d", i+1, topUp))
				fill += topUp
			}
			if fill > capacity {
				fill = capacity
			}
			states = append(states, fill)
		}
		return modelBenchmarkContract(fmt.Sprintf("一个令牌桶初始有 %d 个令牌，容量为 %d。按顺序执行以下动作：%s；每次请求令牌足够才扣除，不足则拒绝本次请求且令牌数不变；每次补充后不超过容量。返回 %d 次动作后每次的桶内令牌数。", initial, capacity, strings.Join(actions, "；"), len(actions))), states
	case "modular-inverse":
		mod := modelBenchmarkPrime(r, []int64{1009, 1013, 1019, 1021, 1031, 1033, 1039, 1049, 1051, 1061, 1063, 1069, 1087, 1091, 1093, 1097, 1103, 1109, 1117, 1123, 1129, 1151, 9973})
		a := int64(r.int(100, int(mod-1)))
		inv := new(big.Int).ModInverse(big.NewInt(a), big.NewInt(mod))
		return modelBenchmarkContract(fmt.Sprintf("返回 %d 在模 %d 下的最小非负乘法逆元，即 answer 必须满足 0 <= answer < %d。", a, mod, mod)), inv.Int64()
	case "markov-expected-steps":
		// 三状态链：期望步数为线性方程组解，不能套用 1/(1-p) 的单一公式。
		p1 := float64(r.int(10, 45)) / 100
		p2 := float64(r.int(10, 45)) / 100
		// E1 = 1/(1-p1) + E2，E2 = 1/(1-p2)：先几何式等待离开 S1，
		// 再几何式等待离开 S2。
		expected := 1/(1-p1) + 1/(1-p2)
		return modelBenchmarkContract(fmt.Sprintf("过程从状态 S1 开始。每步在 S1 以概率 %.2f 留在 S1，否则进入 S2（两者互补）；在 S2 以概率 %.2f 留在 S2，否则进入吸收态 T（两者互补）。所有转移概率整步不变。返回首次到达 T 所需步数的期望，answer 为 JSON 数值，绝对误差不超过 1e-6。", p1, p2)), expected
	case "inclusion-exclusion":
		n := r.int(40, 90)
		a, b, c := r.int(3, 6), r.int(7, 11), r.int(12, 17)
		count := n - n/a - n/b - n/c + n/modelBenchmarkLCM(a, b) + n/modelBenchmarkLCM(a, c) + n/modelBenchmarkLCM(b, c) - n/modelBenchmarkLCM(modelBenchmarkLCM(a, b), c)
		return modelBenchmarkContract(fmt.Sprintf("在 1..%d 中，统计不能被 %d、%d、%d 中任何一个整除的整数个数。", n, a, b, c)), count
	case "two-machine-schedule":
		// 14 个作业（不是 4 个），需要真正的子集划分优化；并列时按升序下标数组字典序取最小。
		jobs := make([]int, 14)
		for i := range jobs {
			jobs[i] = r.int(3, 20)
		}
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
		return modelBenchmarkContract(fmt.Sprintf("将 %d 个作业 %v 分配到两台相同机器，数组元素为各作业耗时；所有作业同时可用、不可拆分或抢占，每台机器串行处理，目标是最小化两台机器总负载的最大值 makespan。作业下标从 0 开始。answer 为 {\"makespan\": VALUE, \"machine_a_indices\": INDICES}；INDICES 必须按升序排列；在所有最优分配中选择该数组字典序最小的方案，比较时按整数逐项比较，若一方是另一方的前缀，则较短者更小。", len(jobs), jobs)), map[string]any{"makespan": bestMakespan, "machine_a_indices": bestIndices}
	case "concurrent-race":
		line := r.int(22, 37)
		safe := r.int(0, 1) == 1
		action, defect := "goroutine 2 并发读取 x，读完后才接收 ch", "data_race"
		if safe {
			action, defect = "goroutine 2 先接收 ch，然后读取 x", "none"
		}
		return modelBenchmarkContract(fmt.Sprintf("在 Go 测试中，普通 int 变量 x 被两个 goroutine 共享。goroutine 1 在第 %d 行写入 x 后向 ch 发信号；%s，没有锁、原子操作或其他同步。判断是否存在数据竞争。answer 为 {\"defect\": 类型, \"safe\": 布尔值, \"line\": 行号}；类型从 data_race、deadlock、none 中选，safe 表示是否无数据竞争，line 填共享变量写入处行号。", line, action)), map[string]any{"defect": defect, "safe": safe, "line": line}
	case "pseudocode-trace":
		// 起始值放大到 3 位数，循环长度与中间大整数都需要逐项追踪。
		start := r.int(100000, 400000)
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
		// 四枚面额加随机金额，贪心与最优的差值需要真正 DP 才能确定。
		coins := []int{1, r.int(3, 6), r.int(7, 10), r.int(11, 15)}
		amount := r.int(40, 240)
		minimum := make([]int, amount+1)
		for value := 1; value <= amount; value++ {
			minimum[value] = amount + 1
			for _, coin := range coins {
				if coin <= value {
					minimum[value] = min(minimum[value], minimum[value-coin]+1)
				}
			}
		}
		sort.Ints(coins)
		remaining, greedy := amount, 0
		for i := len(coins) - 1; i >= 0; i-- {
			greedy += remaining / coins[i]
			remaining %= coins[i]
		}
		return modelBenchmarkContract(fmt.Sprintf("硬币面额为 %v、每种硬币数量不限、金额为 %d；贪心每次选择不超过剩余金额的最大面额时，返回贪心币数与最优币数之差的绝对值。", coins, amount)), greedy - minimum[amount]
	case "truth-assignment":
		// 5 个变量、3 条随机三元子句；唯一解需要完整枚举/消元，
		// 而不是 2 变量试错。数据以结构化的 variables/clauses 给出。
		names := []string{"p", "q", "r", "s", "t"}
		assignment := make([]bool, len(names))
		for i := range assignment {
			assignment[i] = r.int(0, 1) == 1
		}
		literal := func(index int) string {
			if assignment[index] {
				return names[index]
			}
			return "NOT " + names[index]
		}
		randomLiteral := func(index int) string {
			if r.int(0, 1) == 1 {
				return names[index]
			}
			return "NOT " + names[index]
		}
		seen := map[string]bool{}
		clauses := [][]string{}
		for len(clauses) < 3 {
			a, b, c := r.int(0, 4), r.int(0, 4), r.int(0, 4)
			if a == b || b == c || a == c {
				continue
			}
			clause := []string{literal(a), randomLiteral(b), randomLiteral(c)}
			key := strings.Join(clause, "|")
			if seen[key] {
				continue
			}
			seen[key] = true
			clauses = append(clauses, clause)
		}
		// 解空间 32 种；若非唯一，追加只保留隐藏赋值的单字面量子句。
		satisfies := func(clause []string, mask int) bool {
			for _, entry := range clause {
				negated := strings.HasPrefix(entry, "NOT ")
				name := strings.TrimPrefix(entry, "NOT ")
				for i, candidate := range names {
					if candidate == name {
						value := mask>>i&1 == 1
						if negated {
							value = !value
						}
						if value {
							return true
						}
					}
				}
			}
			return false
		}
		solutionCount := func() int {
			count := 0
			for mask := 0; mask < 1<<len(names); mask++ {
				ok := true
				for _, clause := range clauses {
					if !satisfies(clause, mask) {
						ok = false
						break
					}
				}
				if ok {
					count++
				}
			}
			return count
		}
		for solutionCount() > 1 {
			index := len(clauses) % len(names)
			clauses = append(clauses, []string{literal(index)})
		}
		data := map[string]any{"variables": names, "clauses": clauses}
		answer := map[string]any{}
		for i, name := range names {
			answer[name] = assignment[i]
		}
		return modelBenchmarkDataQuestion("5 个布尔变量 variables 满足全部子句：每个子句是若干字面量的析取（OR），字面量形如 \"p\" 表示该变量为真、\"NOT p\" 表示该变量为假；所有子句必须同时为真。求唯一赋值，answer 为含全部变量名的布尔对象。", data), answer
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
		// 14 个候选项，需要逐对检查支配关系；不再是 6 个。
		caseID := r.int(1000, 9999)
		options := [][]int{}
		seen := map[string]bool{}
		for len(options) < 14 {
			a, b := r.int(1, 40), r.int(1, 40)
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
