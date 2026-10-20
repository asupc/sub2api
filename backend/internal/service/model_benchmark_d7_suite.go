package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// D7 families combine several mechanisms in one instance (automata + counting,
// CRDT merge + version order, planning + scheduling). They are still fully
// visible, deterministic and machine-checkable: every solver below reads only
// the public data block and no model-supplied code is executed.
var modelBenchmarkD7Families = []modelBenchmarkFamilySpec{
	{"d7-dfa-minimize", "algorithms", 7, "exact"},
	{"d7-regex-complement", "algorithms", 7, "integer"},
	{"d7-permanent-mod", "math", 7, "integer"},
	{"d7-vector-clock", "code", 7, "sequence"},
	{"d7-crdt-merge", "code", 7, "exact"},
	{"d7-list-scheduling", "planning", 7, "integer"},
	{"d7-datalog-fixpoint", "logic", 7, "set"},
	{"d7-planning-min-steps", "planning", 7, "integer"},
}

func generateModelBenchmarkD7(f modelBenchmarkFamilySpec, r *modelBenchmarkRNG) (string, any) {
	switch f.id {
	case "d7-dfa-minimize":
		data := d7GenDFA(r)
		return modelBenchmarkDataQuestion("确定性有限自动机：状态编号 0..states-1，start 为起始状态，accepting 为接受状态集合，transitions[i] 是状态 i 在输入 0 和 1 上分别转移到的状态。只考虑从 start 可达的状态。字符串从 start 读入后若停在接受状态则被接受，空串长度 0。返回 {minimal_states:删除不可达状态后最小化 DFA 的状态数,shortest_accepted:最短被接受字符串的长度（不存在则 -1）,shortest_rejected:最短被拒绝字符串的长度（不存在则 -1）}。", data), d7DFAAnswer(data)
	case "d7-regex-complement":
		data := d7GenRegex(r)
		return modelBenchmarkDataQuestion("正则表达式语法树：op=lit 表示匹配 lit 指定的单个字符；op=cat 表示按 children 顺序连接；op=alt 表示从 children 中任选一支；op=star 表示 child 出现零次或多次。正则匹配整个字符串，字母表只有 a、b。返回不被该正则匹配的最短字符串长度（整数，空串长度为 0），保证存在这样的字符串。", data), data.shortest
	case "d7-permanent-mod":
		data := d7GenPermanent(r)
		return modelBenchmarkDataQuestion("返回 n×n 整数矩阵 matrix 的积和式（permanent）对 mod 取模后的最小非负余数。积和式定义为对全部 n! 个排列求和 ∏ matrix[i][perm[i]]；与行列式不同，不含符号因子。", data), d7PermanentRyser(data)
	case "d7-vector-clock":
		data := d7GenVectorClock(r)
		return modelBenchmarkDataQuestion("processes 个进程各自维护向量时钟，初值全为 0。事件按 events 顺序发生：事件开始时先把本进程 p 的分量加 1；若 kind=recv，再把 send_index 指向的发送事件当时携带的时钟逐分量取 max；若 kind=send，把加 1 后的完整时钟作为消息时钟登记，供后续 recv 使用；kind=local 不发送也不接收。返回每个事件发生后该事件进程的完整时钟向量，按事件顺序、进程编号 0..processes-1 展平为一个整数序列。", data), d7VectorClocks(data)
	case "d7-crdt-merge":
		data := d7GenCRDT(r)
		return modelBenchmarkDataQuestion("每个副本是 key 到 LWW 寄存器的映射，寄存器含 counter、node、value。版本按 (counter, node) 字典序比较，较大者胜出，node 为字符串按字节序比较。合并多个副本时，对每个 key 取全部副本中版本最大的寄存器，即使该寄存器来自其他副本中较旧的快照。返回合并后每个 key 的 value 组成的 JSON 对象（key 为字符串，value 为整数）；value 不做任何算术合并；counter 相同时按 node 字典序较大者胜出。", data), d7CRDTMerge(data)
	case "d7-list-scheduling":
		data := d7GenListSchedule(r)
		return modelBenchmarkDataQuestion("有 machines 台相同的无限容量并行机，作业从 t=0 起可用；作业必须等待 parents 中全部父作业完成，耗时 duration，不可抢占。调度规则：只要存在空闲机器，就在全部已就绪（父作业均已完成）且未开始的作业中按编号从小到大依次分配给编号最小的空闲机器；没有就绪作业时等待到下一台机器空闲。返回全部作业完成的最早时刻（makespan）。", data), d7ListSchedule(data)
	case "d7-datalog-fixpoint":
		data := d7GenDatalog(r)
		return modelBenchmarkDataQuestion("二元关系 R、S 由 r、s 给出，节点编号 0..nodes-1。以下三条规则取最小不动点反复应用直至不再产生新事实：① R(x,y) 且 S(y,z) 推出 T(x,z)；② S(x,y) 且 T(y,z) 推出 R(x,z)；③ R(x,y) 且 R(y,z) 推出 S(x,z)。返回最终 T 关系中的全部事实，格式为字符串数组，每项为 \"x,y\"；T 为空返回 []。", data), d7DatalogFixpoint(data)
	case "d7-planning-min-steps":
		data := d7GenPlanning(r)
		return modelBenchmarkDataQuestion("STRIPS 规划：predicates 个谓词编号 0..predicates-1，initial 为初始为真的谓词下标。每个动作有前置条件 pre、新增 add、删除 del（同一动作中先删除后新增）。动作只有在 pre 全部为真时可用。目标为 goal 中全部谓词为真（允许其他谓词同时为真）。状态可重复访问但不会带来更短解。返回从 initial 到达目标的最少动作数（整数），不可达返回 -1。", data), d7PlanningSteps(data)
	default:
		panic("unknown d7 benchmark family: " + f.id)
	}
}

// ----- DFA + minimization -----

type d7DFAData struct {
	States      int      `json:"states"`
	Start       int      `json:"start"`
	Accepting   []int    `json:"accepting"`
	Transitions [][2]int `json:"transitions"`
}

func d7GenDFA(r *modelBenchmarkRNG) d7DFAData {
	states := 18
	data := d7DFAData{States: states, Start: r.int(0, 3), Transitions: make([][2]int, states)}
	// 全随机转移，随后覆盖一条骨架链，保证每个状态都从 start 可达
	// （题面仍要求忽略不可达状态，由独立 oracle 复查该语义）。
	for i := 0; i < states; i++ {
		for symbol := 0; symbol < 2; symbol++ {
			data.Transitions[i][symbol] = r.int(0, states-1)
		}
	}
	order := make([]int, 0, states)
	order = append(order, data.Start)
	for i := 0; i < states; i++ {
		if i != data.Start {
			order = append(order, i)
		}
	}
	for i := 0; i+1 < len(order); i++ {
		data.Transitions[order[i]][r.int(0, 1)] = order[i+1]
	}
	accepting := map[int]bool{}
	for len(accepting) == 0 || len(accepting) == states {
		accepting = map[int]bool{}
		for i := 0; i < states; i++ {
			if r.int(0, 2) == 0 {
				accepting[i] = true
			}
		}
	}
	data.Accepting = []int{}
	for i := 0; i < states; i++ {
		if accepting[i] {
			data.Accepting = append(data.Accepting, i)
		}
	}
	return data
}

func d7DFAReachable(data d7DFAData) []bool {
	reachable := make([]bool, data.States)
	queue := []int{data.Start}
	reachable[data.Start] = true
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		for _, next := range data.Transitions[node] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reachable
}

// d7DFAShortest returns the shortest accepted or rejected string length,
// searching the reachable product automaton with BFS.
func d7DFAShortest(data d7DFAData, reachable []bool, wantAccept bool) int {
	isAccepting := make([]bool, data.States)
	for _, state := range data.Accepting {
		if state >= 0 && state < data.States {
			isAccepting[state] = true
		}
	}
	distance := make([]int, data.States)
	for i := range distance {
		distance[i] = -1
	}
	distance[data.Start] = 0
	queue := []int{data.Start}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if reachable[node] && isAccepting[node] == wantAccept {
			return distance[node]
		}
		for _, next := range data.Transitions[node] {
			if distance[next] == -1 {
				distance[next] = distance[node] + 1
				queue = append(queue, next)
			}
		}
	}
	return -1
}

// d7DFAMinimalStates counts Myhill-Nerode classes of reachable states with
// partition refinement (block id + transition block ids).
func d7DFAMinimalStates(data d7DFAData, reachable []bool) int {
	isAccepting := make([]bool, data.States)
	for _, state := range data.Accepting {
		if reachable[state] {
			isAccepting[state] = true
		}
	}
	class := make([]int, data.States)
	for state := 0; state < data.States; state++ {
		if isAccepting[state] {
			class[state] = 1
		}
	}
	// 每轮按 (原分块, 0 转移分块, 1 转移分块) 重新编号；同一分块内只要
	// 有状态被拆开就继续细化，否则已稳定（分块编号本身的重排不算变化）。
	for {
		signature := map[[3]int]int{}
		next := make([]int, data.States)
		for state := 0; state < data.States; state++ {
			if !reachable[state] {
				next[state] = -1
				continue
			}
			key := [3]int{class[state], class[data.Transitions[state][0]], class[data.Transitions[state][1]]}
			id, exists := signature[key]
			if !exists {
				id = len(signature)
				signature[key] = id
			}
			next[state] = id
		}
		split := false
		for a := 0; a < data.States && !split; a++ {
			if !reachable[a] {
				continue
			}
			for b := a + 1; b < data.States; b++ {
				if reachable[b] && class[a] == class[b] && next[a] != next[b] {
					split = true
					break
				}
			}
		}
		class = next
		if !split {
			break
		}
	}
	classes := map[int]bool{}
	for state := 0; state < data.States; state++ {
		if reachable[state] {
			classes[class[state]] = true
		}
	}
	return len(classes)
}

func d7DFAAnswer(data d7DFAData) map[string]any {
	reachable := d7DFAReachable(data)
	return map[string]any{
		"minimal_states":    d7DFAMinimalStates(data, reachable),
		"shortest_accepted": d7DFAShortest(data, reachable, true),
		"shortest_rejected": d7DFAShortest(data, reachable, false),
	}
}

// ----- regex complement -----

type d7RegexNode struct {
	Op       string        `json:"op"`
	Lit      string        `json:"lit,omitempty"`
	Child    *d7RegexNode  `json:"child,omitempty"`
	Children []d7RegexNode `json:"children,omitempty"`
}

type d7RegexQuestion struct {
	Regex    d7RegexNode `json:"regex"`
	shortest int
}

func d7GenRegex(r *modelBenchmarkRNG) d7RegexQuestion {
	var build func(depth int) d7RegexNode
	build = func(depth int) d7RegexNode {
		if depth == 0 || r.int(0, 3) == 0 {
			lit := "a"
			if r.int(0, 1) == 1 {
				lit = "b"
			}
			return d7RegexNode{Op: "lit", Lit: lit}
		}
		switch r.int(0, 2) {
		case 0:
			return d7RegexNode{Op: "cat", Children: []d7RegexNode{build(depth - 1), build(depth - 1)}}
		case 1:
			return d7RegexNode{Op: "alt", Children: []d7RegexNode{build(depth - 1), build(depth - 1)}}
		default:
			child := build(depth - 1)
			return d7RegexNode{Op: "star", Child: &child}
		}
	}
	fallback := func() d7RegexNode {
		star := d7RegexNode{Op: "star", Child: &d7RegexNode{Op: "alt", Children: []d7RegexNode{{Op: "lit", Lit: "a"}, {Op: "lit", Lit: "b"}}}}
		return d7RegexNode{Op: "cat", Children: []d7RegexNode{{Op: "lit", Lit: "a"}, star}}
	}
	for attempt := 0; attempt < 40; attempt++ {
		root := build(3)
		if shortest := d7RegexShortestMiss(root); shortest >= 0 && shortest <= 12 {
			return d7RegexQuestion{Regex: root, shortest: shortest}
		}
	}
	return d7RegexQuestion{Regex: fallback(), shortest: 0}
}

// d7NFA is a Thompson construction with epsilon edges and 2-symbol steps.
type d7NFA struct {
	epsilon [][]int
	step    [][2]int
	accept  int
}

func d7RegexCompile(root d7RegexNode) d7NFA {
	nfa := d7NFA{}
	newState := func() int {
		nfa.epsilon = append(nfa.epsilon, []int{})
		nfa.step = append(nfa.step, [2]int{-1, -1})
		return len(nfa.epsilon) - 1
	}
	var build func(node d7RegexNode) (int, int)
	build = func(node d7RegexNode) (int, int) {
		start, end := newState(), newState()
		switch node.Op {
		case "lit":
			symbol := 0
			if node.Lit == "b" {
				symbol = 1
			}
			nfa.step[start][symbol] = end
		case "cat":
			previous := start
			for _, child := range node.Children {
				childStart, childEnd := build(child)
				nfa.epsilon[previous] = append(nfa.epsilon[previous], childStart)
				previous = childEnd
			}
			nfa.epsilon[previous] = append(nfa.epsilon[previous], end)
		case "alt":
			for _, child := range node.Children {
				childStart, childEnd := build(child)
				nfa.epsilon[start] = append(nfa.epsilon[start], childStart)
				nfa.epsilon[childEnd] = append(nfa.epsilon[childEnd], end)
			}
		case "star":
			innerStart, innerEnd := build(*node.Child)
			nfa.epsilon[start] = append(nfa.epsilon[start], innerStart, end)
			nfa.epsilon[innerEnd] = append(nfa.epsilon[innerEnd], innerStart, end)
		}
		return start, end
	}
	_, end := build(root)
	nfa.accept = end
	return nfa
}

func d7EpsilonClosure(nfa d7NFA, seeds []int) []int {
	seen := make([]bool, len(nfa.epsilon))
	queue := append([]int{}, seeds...)
	for _, state := range queue {
		seen[state] = true
	}
	for head := 0; head < len(queue); head++ {
		for _, next := range nfa.epsilon[queue[head]] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	sort.Ints(queue)
	return queue
}

func d7SetKey(set []int) string {
	parts := make([]string, len(set))
	for i, value := range set {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

// d7RegexShortestMiss determinizes the NFA and BFS-es for the first rejecting
// state; -1 means the regex matches every string.
func d7RegexShortestMiss(root d7RegexNode) int {
	nfa := d7RegexCompile(root)
	// 加一条从接受态回到起始态的 ε 边，使起始闭包成为“已接受前缀”的集合，
	// 否则补集 DFA 的起点会丢掉接受属性。
	nfa.epsilon[nfa.accept] = append(nfa.epsilon[nfa.accept], 0)
	start := d7EpsilonClosure(nfa, []int{0})
	sets := [][]int{start}
	index := map[string]int{d7SetKey(start): 0}
	transitions := [][2]int{}
	isAccepting := func(set []int) bool {
		for _, state := range set {
			if state == nfa.accept {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(sets); i++ {
		var row [2]int
		row[0], row[1] = -1, -1
		for symbol := 0; symbol < 2; symbol++ {
			next := []int{}
			for _, state := range sets[i] {
				if target := nfa.step[state][symbol]; target >= 0 {
					next = append(next, target)
				}
			}
			if len(next) == 0 {
				continue
			}
			closed := d7EpsilonClosure(nfa, next)
			key := d7SetKey(closed)
			id, exists := index[key]
			if !exists {
				id = len(sets)
				index[key] = id
				sets = append(sets, closed)
			}
			row[symbol] = id
		}
		transitions = append(transitions, row)
	}
	distance := make([]int, len(sets))
	for i := range distance {
		distance[i] = -1
	}
	distance[0] = 0
	queue := []int{0}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if !isAccepting(sets[current]) {
			return distance[current]
		}
		for symbol := 0; symbol < 2; symbol++ {
			next := transitions[current][symbol]
			if next < 0 {
				// 缺省转移在完备化后进入永不接受的死状态：该符号即最短拒绝串。
				return distance[current] + 1
			}
			if distance[next] == -1 {
				distance[next] = distance[current] + 1
				queue = append(queue, next)
			}
		}
	}
	return -1
}

// ----- permanent mod p -----

type d7PermanentData struct {
	N      int     `json:"n"`
	Mod    int64   `json:"mod"`
	Matrix [][]int `json:"matrix"`
}

func d7GenPermanent(r *modelBenchmarkRNG) d7PermanentData {
	n := 9
	matrix := make([][]int, n)
	for i := range matrix {
		matrix[i] = make([]int, n)
		for j := range matrix[i] {
			matrix[i][j] = r.int(0, 9)
		}
	}
	return d7PermanentData{N: n, Mod: 1000000007, Matrix: matrix}
}

// d7PermanentRyser applies Ryser's inclusion-exclusion formula modulo Mod.
func d7PermanentRyser(data d7PermanentData) int64 {
	n, mod := data.N, data.Mod
	size := 1 << n
	total := int64(0)
	for mask := 0; mask < size; mask++ {
		product := int64(1)
		for row := 0; row < n && product != 0; row++ {
			sum := int64(0)
			for col := 0; col < n; col++ {
				if mask>>col&1 == 1 {
					sum += int64(data.Matrix[row][col])
				}
			}
			product = product * (sum % mod) % mod
		}
		bits := 0
		for value := mask; value != 0; value &= value - 1 {
			bits++
		}
		if (n-bits)&1 == 1 {
			total = (total - product + mod) % mod
		} else {
			total = (total + product) % mod
		}
	}
	return total % mod
}

// ----- vector clocks -----

type d7VectorEvent struct {
	Process   int    `json:"process"`
	Kind      string `json:"kind"`
	SendIndex int    `json:"send_index"`
}

type d7VectorData struct {
	Processes int             `json:"processes"`
	Events    []d7VectorEvent `json:"events"`
}

func d7GenVectorClock(r *modelBenchmarkRNG) d7VectorData {
	processes := 4
	data := d7VectorData{Processes: processes}
	pending := []int{}
	for i := 0; i < 64; i++ {
		process := r.int(0, processes-1)
		kind, sendIndex := "local", 0
		switch r.int(0, 3) {
		case 1:
			// 先挑选发送方与接收方不同的消息，再决定动作类型；所有消息
			// 都尚未被接收，接收后从待接收列表移除。
			crossProcess := []int{}
			for _, index := range pending {
				if data.Events[index].Process != process {
					crossProcess = append(crossProcess, index)
				}
			}
			if len(crossProcess) > 0 && r.int(0, 1) == 0 {
				sendIndex = crossProcess[r.int(0, len(crossProcess)-1)]
				for position, index := range pending {
					if index == sendIndex {
						pending = append(pending[:position], pending[position+1:]...)
						break
					}
				}
				kind = "recv"
			} else {
				kind = "send"
			}
		}
		if kind == "send" {
			pending = append(pending, i)
		}
		data.Events = append(data.Events, d7VectorEvent{Process: process, Kind: kind, SendIndex: sendIndex})
	}
	return data
}

func d7VectorClocks(data d7VectorData) []int {
	clocks := make([][]int, data.Processes)
	for i := range clocks {
		clocks[i] = make([]int, data.Processes)
	}
	sent := map[int][]int{}
	out := make([]int, 0, len(data.Events)*data.Processes)
	for index, event := range data.Events {
		clock := clocks[event.Process]
		clock[event.Process]++
		if event.Kind == "recv" {
			if message, ok := sent[event.SendIndex]; ok {
				for i := range clock {
					clock[i] = max(clock[i], message[i])
				}
			}
		}
		if event.Kind == "send" {
			sent[index] = append([]int{}, clock...)
		}
		out = append(out, clock...)
	}
	return out
}

// ----- CRDT LWW map merge -----

type d7Register struct {
	Counter int64  `json:"counter"`
	Node    string `json:"node"`
	Value   int64  `json:"value"`
}

type d7CRDTData struct {
	Replicas []map[string]d7Register `json:"replicas"`
}

func d7GenCRDT(r *modelBenchmarkRNG) d7CRDTData {
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	replicas := make([]map[string]d7Register, 4)
	for replica := range replicas {
		replicas[replica] = map[string]d7Register{}
		for _, key := range keys {
			if r.int(0, 3) == 0 {
				continue
			}
			// node 在每个副本内唯一，保证 (counter, node) 全序无歧义。
			node := fmt.Sprintf("n%d", replica)
			replicas[replica][key] = d7Register{Counter: int64(r.int(0, 20)), Node: node, Value: int64(r.int(-50, 50))}
		}
	}
	return d7CRDTData{Replicas: replicas}
}

func d7CRDTMerge(data d7CRDTData) map[string]any {
	best := map[string]d7Register{}
	for _, replica := range data.Replicas {
		for key, register := range replica {
			old, exists := best[key]
			if !exists || register.Counter > old.Counter || (register.Counter == old.Counter && register.Node > old.Node) {
				best[key] = register
			}
		}
	}
	out := map[string]any{}
	for key, register := range best {
		out[key] = register.Value
	}
	return out
}

// ----- list scheduling with precedence -----

type d7ScheduleJob struct {
	Duration int   `json:"duration"`
	Parents  []int `json:"parents"`
}

type d7ScheduleData struct {
	Machines int             `json:"machines"`
	Jobs     []d7ScheduleJob `json:"jobs"`
}

func d7GenListSchedule(r *modelBenchmarkRNG) d7ScheduleData {
	jobs := make([]d7ScheduleJob, 20)
	for i := range jobs {
		parents := []int{}
		if i > 0 {
			parents = append(parents, r.int(0, i-1))
			if i > 3 && r.int(0, 2) == 0 {
				parent := r.int(0, i-1)
				if parent != parents[0] {
					parents = append(parents, parent)
				}
			}
		}
		jobs[i] = d7ScheduleJob{Duration: r.int(2, 9), Parents: parents}
	}
	return d7ScheduleData{Machines: 3, Jobs: jobs}
}

func d7ListSchedule(data d7ScheduleData) int {
	n := len(data.Jobs)
	machineFree := make([]int, data.Machines)
	scheduled := make([]bool, n)
	finish := make([]int, n)
	time, done := 0, 0
	for done < n {
		ready := []int{}
		for i := 0; i < n; i++ {
			if scheduled[i] {
				continue
			}
			blocked := false
			for _, parent := range data.Jobs[i].Parents {
				if !scheduled[parent] || finish[parent] > time {
					blocked = true
					break
				}
			}
			if !blocked {
				ready = append(ready, i)
			}
		}
		free := []int{}
		for m := 0; m < data.Machines; m++ {
			if machineFree[m] <= time {
				free = append(free, m)
			}
		}
		if len(ready) > 0 && len(free) > 0 {
			for k := 0; k < min(len(ready), len(free)); k++ {
				job, machine := ready[k], free[k]
				finish[job] = time + data.Jobs[job].Duration
				machineFree[machine] = finish[job]
				scheduled[job] = true
				done++
			}
			continue
		}
		next := -1
		for _, value := range machineFree {
			if value > time && (next == -1 || value < next) {
				next = value
			}
		}
		if next == -1 {
			break
		}
		time = next
	}
	makespan := 0
	for _, value := range finish {
		makespan = max(makespan, value)
	}
	return makespan
}

// ----- Datalog least fixed point -----

type d7DatalogData struct {
	Nodes int      `json:"nodes"`
	R     [][2]int `json:"r"`
	S     [][2]int `json:"s"`
}

func d7GenDatalog(r *modelBenchmarkRNG) d7DatalogData {
	nodes := 12
	data := d7DatalogData{Nodes: nodes}
	seenR, seenS := map[[2]int]bool{}, map[[2]int]bool{}
	for len(data.R) < 18 {
		pair := [2]int{r.int(0, nodes-1), r.int(0, nodes-1)}
		if !seenR[pair] {
			seenR[pair] = true
			data.R = append(data.R, pair)
		}
	}
	for len(data.S) < 14 {
		pair := [2]int{r.int(0, nodes-1), r.int(0, nodes-1)}
		if !seenS[pair] {
			seenS[pair] = true
			data.S = append(data.S, pair)
		}
	}
	return data
}

func d7DatalogFixpoint(data d7DatalogData) []string {
	relations := map[[2]int]bool{}
	symmetric := map[[2]int]bool{}
	derived := map[[2]int]bool{}
	for _, pair := range data.R {
		relations[pair] = true
	}
	for _, pair := range data.S {
		symmetric[pair] = true
	}
	for changed := true; changed; {
		changed = false
		add := func(target map[[2]int]bool, pair [2]int) {
			if !target[pair] {
				target[pair] = true
				changed = true
			}
		}
		for a := range relations {
			for b := range symmetric {
				if a[1] == b[0] {
					add(derived, [2]int{a[0], b[1]})
				}
			}
		}
		for a := range symmetric {
			for b := range derived {
				if a[1] == b[0] {
					add(relations, [2]int{a[0], b[1]})
				}
			}
		}
		for a := range relations {
			for b := range relations {
				if a[1] == b[0] {
					add(symmetric, [2]int{a[0], b[1]})
				}
			}
		}
	}
	out := []string{}
	for pair := range derived {
		out = append(out, fmt.Sprintf("%d,%d", pair[0], pair[1]))
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := strings.Split(out[i], ","), strings.Split(out[j], ",")
		lx, _ := strconv.Atoi(left[0])
		rx, _ := strconv.Atoi(right[0])
		if lx != rx {
			return lx < rx
		}
		ly, _ := strconv.Atoi(left[1])
		ry, _ := strconv.Atoi(right[1])
		return ly < ry
	})
	return out
}

// ----- STRIPS planning -----

type d7Action struct {
	Pre []int `json:"pre"`
	Add []int `json:"add"`
	Del []int `json:"del"`
}

type d7PlanningData struct {
	Predicates int        `json:"predicates"`
	Initial    []int      `json:"initial"`
	Goal       []int      `json:"goal"`
	Actions    []d7Action `json:"actions"`
}

func d7GenPlanning(r *modelBenchmarkRNG) d7PlanningData {
	// 目标从可达状态反推，通常一两轮即有非零步解；上限只用于防御异常随机源。
	for attempt := 0; attempt < 64; attempt++ {
		data := d7GenPlanningAttempt(r)
		if len(data.Goal) > 0 && d7PlanningSteps(data) > 0 {
			return data
		}
	}
	// 兜底：直接构造初始 → 目标的单步实例，保证生成器一定终止。
	initial := []int{0}
	return d7PlanningData{Predicates: 12, Initial: initial, Goal: []int{1}, Actions: []d7Action{{Pre: []int{0}, Add: []int{1}, Del: []int{}}}}
}

func d7GenPlanningAttempt(r *modelBenchmarkRNG) d7PlanningData {
	predicates := 12
	initial := []int{r.int(0, 3), r.int(4, 7)}
	actions := make([]d7Action, 18)
	for i := range actions {
		action := d7Action{}
		used := map[int]bool{}
		preCount := r.int(1, 2)
		for attempts := 0; len(action.Pre) < preCount && attempts < 40; attempts++ {
			value := r.int(0, predicates-1)
			if !used[value] {
				used[value] = true
				action.Pre = append(action.Pre, value)
			}
		}
		addCount := r.int(1, 2)
		for attempts := 0; len(action.Add) < addCount && attempts < 40; attempts++ {
			value := r.int(0, predicates-1)
			if !used[value] {
				used[value] = true
				action.Add = append(action.Add, value)
			}
		}
		deleteCount := r.int(0, 1)
		attempts := 0
		for len(action.Del) < deleteCount && attempts < 20 {
			attempts++
			value := r.int(0, predicates-1)
			if !used[value] {
				used[value] = true
				action.Del = append(action.Del, value)
			}
		}
		actions[i] = action
	}
	data := d7PlanningData{Predicates: predicates, Initial: initial, Actions: actions}
	// 从初始状态可达的状态中选一个作为目标，保证一定存在解。
	reachable := map[int]bool{d7PlanMask(initial): true}
	queue := []int{d7PlanMask(initial)}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		for _, action := range actions {
			pre := d7PlanMask(action.Pre)
			if state&pre != pre {
				continue
			}
			next := (state &^ d7PlanMask(action.Del)) | d7PlanMask(action.Add)
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	candidates := []int{}
	for state := range reachable {
		if state != d7PlanMask(initial) && state != 0 {
			candidates = append(candidates, state)
		}
	}
	if len(candidates) == 0 {
		return d7PlanningData{Predicates: predicates, Initial: initial, Actions: actions}
	}
	sort.Ints(candidates)
	target := candidates[r.int(0, len(candidates)-1)]
	// 目标只保留不在初始状态中的谓词，避免目标被初始状态直接满足。
	subset := []int{}
	for bit := 0; bit < predicates; bit++ {
		if target>>bit&1 == 1 && d7PlanMask(initial)>>bit&1 == 0 {
			subset = append(subset, bit)
		}
	}
	if len(subset) > 0 {
		data.Goal = subset
	} else {
		for bit := 0; bit < predicates; bit++ {
			if target>>bit&1 == 1 {
				data.Goal = append(data.Goal, bit)
			}
		}
	}
	return data
}

func d7PlanMask(values []int) int {
	mask := 0
	for _, value := range values {
		mask |= 1 << value
	}
	return mask
}

func d7PlanningSteps(data d7PlanningData) int {
	initial := d7PlanMask(data.Initial)
	goal := d7PlanMask(data.Goal)
	if initial&goal == goal {
		return 0
	}
	distance := map[int]int{initial: 0}
	queue := []int{initial}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		for _, action := range data.Actions {
			pre := d7PlanMask(action.Pre)
			if state&pre != pre {
				continue
			}
			next := (state &^ d7PlanMask(action.Del)) | d7PlanMask(action.Add)
			if _, seen := distance[next]; seen {
				continue
			}
			distance[next] = distance[state] + 1
			if next&goal == goal {
				return distance[next]
			}
			queue = append(queue, next)
		}
	}
	return -1
}
