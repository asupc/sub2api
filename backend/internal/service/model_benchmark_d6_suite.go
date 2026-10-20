package service

import (
	"sort"
)

// D6 families raise input scale so frontier models must actually search,
// enumerate or plan rather than pattern-match a draft-sized instance.
// All state and rules stay visible in the public data block; expected values
// are deterministic and machine-checkable, and witness families accept any
// optimal answer verified for feasibility plus optimal value.
var modelBenchmarkD6Families = []modelBenchmarkFamilySpec{
	// algorithms
	{"d6-maxflow-cut-count", "algorithms", 6, "fields"},
	{"d6-scc-condensation", "algorithms", 6, "fields"},
	{"d6-bitmask-tsp", "algorithms", 6, "integer"},
	{"d6-interval-dp-count", "algorithms", 6, "fields"},
	{"d6-shortest-path-k", "algorithms", 6, "integer"},
	{"d6-topo-count", "algorithms", 6, "integer"},
	{"d6-lis-count", "algorithms", 6, "fields"},
	// code / systems
	{"d6-gc-trace", "code", 6, "fields"},
	{"d6-bytecode-vm", "code", 6, "fields"},
	{"d6-register-overflow", "code", 6, "sequence"},
	{"d6-serializability", "code", 6, "fields"},
	{"d6-lru-writeback", "code", 6, "fields"},
	// logic / planning
	{"d6-sat-witness", "logic", 6, "witness"},
	{"d6-epistemic-puzzle", "logic", 6, "set"},
	{"d6-route-windows", "planning", 6, "witness"},
	{"d6-resource-alloc", "planning", 6, "witness"},
	{"d6-matrix-chain", "algorithms", 6, "integer"},
	{"d6-nim-game", "logic", 6, "exact"},
}

func generateModelBenchmarkD6(f modelBenchmarkFamilySpec, r *modelBenchmarkRNG) (string, any) {
	switch f.id {
	// ----- algorithms -----
	case "d6-maxflow-cut-count":
		data := benchmarkRandomCodingGraph(r, 16, 1, 9)
		return modelBenchmarkDataQuestion("有向容量图，cost表示容量，need忽略。源点0，汇点nodes-1。割由包含源且不含汇的顶点集合S唯一确定，容量为所有S到其补集的边容量之和（反向边不计）。返回 {flow:最大流值,cut_count:达到最小割容量的不同S数量}。没有源汇路径时最大流为0，仍计算全部最小割。", data), benchmarkFlowCuts(data)
	case "d6-scc-condensation":
		data := benchmarkRandomCodingGraph(r, 30, 1, 1)
		return modelBenchmarkDataQuestion("求有向图强连通分量，cost/need均忽略。返回 {components:[[每分量节点升序],...],edges_needed:最少需新增多少条任意有向边才能使整图强连通}；components按各分量最小节点升序，已有强连通图需0条。不要求返回具体新增边。", data), benchmarkSCC(data)
	case "d6-bitmask-tsp":
		n := 14
		dist := make([][]int, n)
		for i := range dist {
			dist[i] = make([]int, n)
			for j := 0; j < i; j++ {
				dist[i][j] = r.int(2, 25)
				dist[j][i] = dist[i][j]
			}
		}
		return modelBenchmarkDataQuestion("旅行商：n 个城市（0-based），dist 是对称距离矩阵。从城市 0 出发，访问每个城市恰好一次并回到 0，最小化总行程。返回答案为最小总距离的整数。", map[string]any{"n": n, "dist": dist}), d6TSP(dist)
	case "d6-interval-dp-count":
		n := 16
		values := benchmarkCodingInts(r, n, 1, 9)
		return modelBenchmarkDataQuestion("n 堆石子排成一排，值为 values。每次合并相邻两堆，代价是新堆的值（两堆值之和），合并后占据原位置继续参与。合并到只剩一堆的最小总代价为 cost；在所有达到最小总代价的合并方案中，统计不同的完整合并方案数量 count（方案按每次合并选择的位置序列区分）。返回 {cost:最小总代价,count:最优方案数}。", map[string]any{"values": values}), d6IntervalCount(values)
	case "d6-shortest-path-k":
		data := benchmarkRandomCodingGraph(r, 14, 1, 9)
		k := r.int(4, 7)
		return modelBenchmarkDataQuestion("有向带权图，cost为边权，need忽略。求从节点0到节点nodes-1、**恰好经过 k 条边**的路径的最小总cost（允许重复经过节点和边）。若不存在恰好 k 条边的路径，返回 -1。返回整数。", map[string]any{"nodes": data.Nodes, "edges": data.Edges, "k": k}), d6ShortestK(data, k)
	case "d6-topo-count":
		n := 12
		edges := d6RandomDAG(r, n)
		return modelBenchmarkDataQuestion("有向无环图 nodes 个节点（0-based），edges 为 [from,to] 依赖（from 必须先于 to）。返回不同的拓扑排序总数（整数）。", map[string]any{"nodes": n, "edges": edges}), d6TopoCount(n, edges)
	case "d6-lis-count":
		values := benchmarkCodingInts(r, 80, -40, 60)
		return modelBenchmarkDataQuestion("给定整数数组，求严格递增子序列（不要求连续）。返回 {length:最长长度,count:达到最长长度的子序列数量}。按选中的下标集合区分，相同值但下标不同分别计数，不能把相等值相接。", map[string]any{"values": values}), d6LIS(values)
	// ----- code / systems -----
	case "d6-gc-trace":
		data := d6GenGCData(r)
		return modelBenchmarkDataQuestion("三色标记垃圾回收模拟：对象为 0..count-1，edges 为 [from,to] 引用，roots 为根集合。所有对象初始白色。标记阶段：从根开始把可达对象标灰，每次取一个灰色对象标黑并把它指向的白色对象标灰，直到无灰色。随后清扫阶段回收所有仍是白色的对象。返回 {marked:[最终被标黑的对象编号升序],collected:[被回收的白色对象编号升序],gray_steps:标记过程中由白变灰的总次数}。", data), d6GC(data)
	case "d6-bytecode-vm":
		data := d6BytecodeProgram(r)
		return modelBenchmarkDataQuestion("栈式虚拟机：PC=0、数据栈初始为空、调用栈初始为空，每个栈帧有私有 locals 数组（长度与初始 locals 相同）。load i 压入当前帧locals[i]；push n 压常数；store i 弹栈写入当前帧locals[i]；二元 op 先弹 b 后弹 a，压 a op b（mod 整数余数，xor 位异或）。jz 弹栈若为0跳到arg否则PC+1；jmp 无条件跳；call arg 先弹数据栈顶作为被调函数参数，把{返回地址=当前PC+1, 当前locals快照}压入调用栈，再以全新 locals（locals[0]=参数，其余为0）跳到 arg；ret 先弹数据栈顶作为返回值，弹出调用栈顶恢复返回地址与该帧 locals，再把返回值压回数据栈；halt 停机。每执行一条指令计1步，含halt；其余指令PC+1。整数无限精度不溢出。返回 {locals:[停机时当前帧局部变量],steps:步数}。", data), d6Bytecode(data)
	case "d6-register-overflow":
		data := d6OverflowData(r)
		return modelBenchmarkDataQuestion("按 bits 位二进制补码寄存器执行。add/mul/xor 对arg运算后仅保留低bits位；shr 为符号扩展的算术右移，ushr 为把寄存器视为无符号数的逻辑右移。每步均将结果解释成 bits 位有符号整数。返回每次操作后的值数组（不含initial）。", data), benchmarkOverflow(data)
	case "d6-serializability":
		data := d6ConflictData(r)
		return modelBenchmarkDataQuestion("operations 是从早到晚的数据库操作，tx 为事务编号，key 为数据项，write=false 表示读。不同事务操作同一 key 且至少一个写时，给先操作的事务到后操作的事务连依赖边。返回 {serializable:冲突图是否无环,cycle_members:[属于至少一个有向环的全部事务编号，升序]}。同一事务的操作不连自环。", data), benchmarkConflictGraph(data)
	case "d6-lru-writeback":
		data := d6LRUData(r)
		return modelBenchmarkDataQuestion("空缓存按 operations 访问。命中移到 MRU；缺页先按 LRU 淘汰（满时），再载入并移到 MRU。write=true 将页标脏，读不会清除脏标记；淘汰脏页计一次写回，结束时不额外刷盘。返回 {misses:缺页数,writebacks:写回数,order:[最终LRU到MRU的key],dirty:[最终脏页key，升序]}。", data), benchmarkLRU(data)
	// ----- logic / planning -----
	case "d6-sat-witness":
		data := d6GenSATData(r)
		return modelBenchmarkDataQuestion("布尔可满足性：vars 个变量（下标 0..vars-1），clauses 为子句数组，每个子句是若干字面量数组，正整数 i+1 表示变量 i 为真、-(i+1) 表示变量 i 为假；子句内取或、子句间取与。返回使全部子句为真的变量赋值：answer 为长度 vars 的布尔数组（下标 i 对应变量 i 的真值）。保证有解；任一满足赋值均接受。", data), d6SATReference(data)
	case "d6-epistemic-puzzle":
		data := d6GenEpistemicData(r)
		return modelBenchmarkDataQuestion("5 人围坐... (公开信息：seen_counts[i] 表示 agent i 公开声称自己看到的钥匙数量；至少一人持有、至少一人不持有)。根据公开 seen_counts 推断最终能确定自己持有钥匙的人。返回编号数组。", map[string]any{"agents": data.Agents, "seen_counts": data.SeenCounts}), d6EpistemicAnswer(data)
	case "d6-route-windows":
		data := d6GenRouteData(r)
		return modelBenchmarkDataQuestion("从节点 0 在 t=0 出发，节点 1..N 各访问一次，不需返回 0。travel 是对称耗时矩阵（含节点0），windows[i-1]=[最早服务开始,最晚服务开始]；早到需等待，超最晚开始则不可行；每点服务 service 个时间单位。最小化最后服务结束时刻。返回 {\"order\":访问节点数组,\"finish_time\":时刻}，任一最优序列均接受；无可行解返回 order=[]、finish_time=-1。", data), d6BestRoute(data)
	case "d6-resource-alloc":
		data := d6GenResourceData(r)
		return modelBenchmarkDataQuestion("每项最多选一次，每个维度 d 的总用量不超过 capacities[d]，最大化总 value。返回被选项目的 0-based 下标升序数组。所有达到最大 value 的可行子集均接受。", data), d6BestResource(data)
	case "d6-matrix-chain":
		data := d6GenMatrixChain(r)
		return modelBenchmarkDataQuestion("矩阵链乘法：dims 长度 n+1，第 i 个矩阵（0-based）维度为 dims[i]×dims[i+1]。两个矩阵 A(p×q) 与 B(q×r) 相乘的标量乘法代价为 p*q*r。求把 n 个矩阵按给定顺序完全括号化、使总标量乘法次数最小的最小代价（整数）。", data), d6MatrixChain(data)
	case "d6-nim-game":
		data := d6GenNimData(r)
		return modelBenchmarkDataQuestion("Nim 博弈：heaps 为各堆石子数。两名玩家轮流从任选一堆取走至少 1 枚石子，取走最后一枚者胜（正常规则）。双方最优。返回先手必胜时的一个合法最优首着 [heap_index, remove_count]；先手必败返回 []。", data), d6Nim(data)
	default:
		return generateModelBenchmarkD7(f, r)
	}
}

// ----- algorithm solvers -----

// d6TSP solves the symmetric travelling-salesman tour from node 0 with a
// Held-Karp bitmask DP. n stays small enough (<=11) for 2^n * n states.
func d6TSP(dist [][]int) int {
	n := len(dist)
	const inf = 1 << 30
	size := 1 << n
	dp := make([][]int, size)
	for i := range dp {
		dp[i] = make([]int, n)
		for j := range dp[i] {
			dp[i][j] = inf
		}
	}
	dp[1][0] = 0
	for mask := 1; mask < size; mask++ {
		for last := 0; last < n; last++ {
			cur := dp[mask][last]
			if cur == inf || mask>>last&1 == 0 {
				continue
			}
			for next := 0; next < n; next++ {
				if mask>>next&1 == 1 {
					continue
				}
				nm := mask | 1<<next
				if v := cur + dist[last][next]; v < dp[nm][next] {
					dp[nm][next] = v
				}
			}
		}
	}
	full := size - 1
	best := inf
	for last := 1; last < n; last++ {
		if v := dp[full][last] + dist[last][0]; v < best {
			best = v
		}
	}
	return best
}

// d6IntervalCount returns the minimum total merge cost and the number of
// distinct optimal merge sequences for n piles in a row (adjacent merges).
func d6IntervalCount(values []int) map[string]any {
	n := len(values)
	prefix := make([]int, n+1)
	for i, v := range values {
		prefix[i+1] = prefix[i] + v
	}
	sum := func(i, j int) int { return prefix[j+1] - prefix[i] }
	const inf = int64(1) << 60
	cost := make([][]int64, n)
	count := make([][]int64, n)
	for i := range cost {
		cost[i] = make([]int64, n)
		count[i] = make([]int64, n)
		count[i][i] = 1
	}
	for length := 2; length <= n; length++ {
		for i := 0; i+length-1 < n; i++ {
			j := i + length - 1
			cost[i][j] = inf
			for k := i; k < j; k++ {
				c := cost[i][k] + cost[k+1][j] + int64(sum(i, j))
				ways := count[i][k] * count[k+1][j]
				if c < cost[i][j] {
					cost[i][j] = c
					count[i][j] = ways
				} else if c == cost[i][j] {
					count[i][j] += ways
				}
			}
		}
	}
	return map[string]any{"cost": cost[0][n-1], "count": count[0][n-1]}
}

// d6ShortestK is the min cost from 0 to n-1 using exactly k edges, via a
// layered DP over (node, edgesUsed). Revisits allowed.
func d6ShortestK(data benchmarkCodingGraph, k int) int {
	const inf = 1 << 30
	n := data.Nodes
	dist := make([][]int, k+1)
	for i := range dist {
		dist[i] = make([]int, n)
		for j := range dist[i] {
			dist[i][j] = inf
		}
	}
	dist[0][0] = 0
	for e := 0; e < k; e++ {
		for _, edge := range data.Edges {
			if dist[e][edge.From] != inf {
				if v := dist[e][edge.From] + edge.Cost; v < dist[e+1][edge.To] {
					dist[e+1][edge.To] = v
				}
			}
		}
	}
	if dist[k][n-1] == inf {
		return -1
	}
	return dist[k][n-1]
}

// d6RandomDAG builds a random DAG on n nodes by only adding edges i->j (i<j).
func d6RandomDAG(r *modelBenchmarkRNG, n int) [][2]int {
	edges := [][2]int{}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if r.int(0, 3) == 0 {
				edges = append(edges, [2]int{i, j})
			}
		}
	}
	return edges
}

// d6TopoCount counts topological orderings of a DAG via subset DP.
func d6TopoCount(n int, edges [][2]int) int64 {
	indeg := make([]int, n)
	adj := make([][]int, n)
	for _, e := range edges {
		adj[e[0]] = append(adj[e[0]], e[1])
		indeg[e[1]]++
	}
	size := 1 << n
	dp := make([]int64, size)
	dp[0] = 1
	for mask := 0; mask < size; mask++ {
		if dp[mask] == 0 {
			continue
		}
		// A node is placeable next if all its predecessors are in mask.
		for node := 0; node < n; node++ {
			if mask>>node&1 == 1 {
				continue
			}
			ready := true
			for _, e := range edges {
				if e[1] == node && mask>>e[0]&1 == 0 {
					ready = false
					break
				}
			}
			if ready {
				dp[mask|1<<node] += dp[mask]
			}
		}
	}
	return dp[size-1]
}

// d6LIS is the O(n^2) DP for length and count of strict LIS on n=40.
func d6LIS(values []int) map[string]any {
	n := len(values)
	lengths, counts := make([]int, n), make([]int64, n)
	best, total := 0, int64(0)
	for i, value := range values {
		lengths[i], counts[i] = 1, 1
		for j := 0; j < i; j++ {
			if values[j] < value {
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
}

// ----- code / systems solvers -----

// d6GCData builds a tri-color mark-and-sweep instance.
type d6GCData struct {
	Count int      `json:"count"`
	Edges [][2]int `json:"edges"`
	Roots []int    `json:"roots"`
}

func d6GenGCData(r *modelBenchmarkRNG) d6GCData {
	count := r.int(24, 32)
	edges := [][2]int{}
	for i := 0; i < count; i++ {
		// each node points to 0-2 others, may create cycles and garbage islands
		for k := 0; k < r.int(0, 2); k++ {
			to := r.int(0, count-1)
			if to != i {
				edges = append(edges, [2]int{i, to})
			}
		}
	}
	rootCount := r.int(1, 3)
	roots := []int{}
	seen := map[int]bool{}
	for len(roots) < rootCount {
		x := r.int(0, count-1)
		if !seen[x] {
			seen[x] = true
			roots = append(roots, x)
		}
	}
	sort.Ints(roots)
	return d6GCData{Count: count, Edges: edges, Roots: roots}
}

// d6GC runs tri-color marking from roots, then sweeps white objects.
func d6GC(data d6GCData) map[string]any {
	const white, gray, black = 0, 1, 2
	color := make([]int, data.Count)
	adj := make([][]int, data.Count)
	for _, e := range data.Edges {
		adj[e[0]] = append(adj[e[0]], e[1])
	}
	queue := []int{}
	graySteps := 0
	mark := func(x int) {
		if color[x] == white {
			color[x] = gray
			graySteps++
			queue = append(queue, x)
		}
	}
	for _, rt := range data.Roots {
		mark(rt)
	}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		color[x] = black
		for _, nx := range adj[x] {
			mark(nx)
		}
	}
	marked, collected := []int{}, []int{}
	for i := 0; i < data.Count; i++ {
		if color[i] == black {
			marked = append(marked, i)
		} else {
			collected = append(collected, i)
		}
	}
	return map[string]any{"marked": marked, "collected": collected, "gray_steps": graySteps}
}

type d6BytecodeFrame struct {
	ReturnPC int
	Locals   []int
}
type d6BytecodeData struct {
	Locals  []int                  `json:"locals"`
	Program []benchmarkInstruction `json:"program"`
}

// d6BytecodeProgram builds a deterministic program that computes the countdown
// sum f(n)=n+f(n-1) recursively, f(0)=0, exercising call frames with private
// locals and a value passed/returned through the data stack.
func d6BytecodeProgram(r *modelBenchmarkRNG) d6BytecodeData {
	n := r.int(8, 12)
	locals := []int{n, 0, 0, 0}
	// Semantics (declared in the prompt): call pops the data-stack top as the
	// callee's argument -> callee frame's locals[0]; ret pops the data-stack top
	// as the return value, restores the caller frame, pushes the value back.
	// Layout:
	//   0: load 0     ; main: push n
	//   1: call 4     ; -> f, f(n) pushed back on the data stack
	//   2: store 2    ; main.locals[2] = f(n)
	//   3: halt       ; report main frame locals
	//   4: load 0     ; f: push n (callee locals[0] = arg)
	//   5: jz 13      ; n==0 -> base
	//   6: load 0     ; push n
	//   7: push 1
	//   8: sub        ; n-1
	//   9: call 4     ; f(n-1) on stack
	//   10: load 0    ; push n
	//   11: add       ; f(n-1) + n == n + f(n-1)
	//   12: ret
	//   13: push 0
	//   14: ret
	program := []benchmarkInstruction{
		{Op: "load", Arg: 0}, {Op: "call", Arg: 4}, {Op: "store", Arg: 2}, {Op: "halt", Arg: 0},
		{Op: "load", Arg: 0}, {Op: "jz", Arg: 13}, {Op: "load", Arg: 0}, {Op: "push", Arg: 1}, {Op: "sub", Arg: 0}, {Op: "call", Arg: 4}, {Op: "load", Arg: 0}, {Op: "add", Arg: 0}, {Op: "ret", Arg: 0},
		{Op: "push", Arg: 0}, {Op: "ret", Arg: 0},
	}
	return d6BytecodeData{Locals: locals, Program: program}
}

// d6Bytecode executes the stack VM with an explicit call stack whose frames
// hold private locals. The data stack passes the argument in and the result out.
func d6Bytecode(data d6BytecodeData) map[string]any {
	locals := append([]int{}, data.Locals...)
	stack, callstack := []int{}, []d6BytecodeFrame{}
	steps := 0
	pop := func() int { v := stack[len(stack)-1]; stack = stack[:len(stack)-1]; return v }
	for pc := 0; ; {
		instr := data.Program[pc]
		steps++
		pc++
		switch instr.Op {
		case "load":
			stack = append(stack, locals[instr.Arg])
		case "push":
			stack = append(stack, instr.Arg)
		case "store":
			locals[instr.Arg] = pop()
		case "add", "sub", "mod", "xor":
			b, a := pop(), pop()
			v := a + b
			switch instr.Op {
			case "sub":
				v = a - b
			case "mod":
				v = a % b
			case "xor":
				v = a ^ b
			}
			stack = append(stack, v)
		case "jz":
			if pop() == 0 {
				pc = instr.Arg
			}
		case "jmp":
			pc = instr.Arg
		case "call":
			// Save caller frame (return PC + private locals snapshot), start a
			// fresh frame whose locals[0] is the popped argument.
			arg := pop()
			callstack = append(callstack, d6BytecodeFrame{ReturnPC: pc, Locals: append([]int{}, locals...)})
			locals = make([]int, len(locals))
			locals[0] = arg
			pc = instr.Arg
		case "ret":
			ret := pop()
			frame := callstack[len(callstack)-1]
			callstack = callstack[:len(callstack)-1]
			locals = frame.Locals
			pc = frame.ReturnPC
			stack = append(stack, ret)
		case "halt":
			return map[string]any{"locals": locals, "steps": steps}
		}
	}
}

// d6OverflowData scales register width to 16 bits and 40 operations.
func d6OverflowData(r *modelBenchmarkRNG) benchmarkOverflowData {
	data := benchmarkOverflowData{Bits: 16, Initial: r.int(-20000, 20000), Operations: []benchmarkInstruction{}}
	ops := []string{"add", "mul", "xor", "shr", "ushr"}
	for i := 0; i < 80; i++ {
		op := ops[r.int(0, len(ops)-1)]
		arg := r.int(2, 9)
		if op == "shr" || op == "ushr" {
			arg = r.int(1, 6)
		}
		data.Operations = append(data.Operations, benchmarkInstruction{op, arg})
	}
	return data
}

// d6ConflictData scales the schedule to 8 transactions and 40 operations.
func d6ConflictData(r *modelBenchmarkRNG) benchmarkConflictData {
	data := benchmarkConflictData{Transactions: 12, Operations: []benchmarkDBOperation{}}
	for i := 0; i < 80; i++ {
		data.Operations = append(data.Operations, benchmarkDBOperation{Tx: r.int(0, 11), Key: r.int(0, 5), Write: r.int(0, 1) == 1})
	}
	return data
}

// d6LRUData scales to capacity 8 and 60 operations.
func d6LRUData(r *modelBenchmarkRNG) benchmarkLRUData {
	data := benchmarkLRUData{Capacity: r.int(5, 8), Operations: []benchmarkCacheOperation{}}
	for i := 0; i < 120; i++ {
		data.Operations = append(data.Operations, benchmarkCacheOperation{Key: r.int(0, 15), Write: r.int(0, 2) == 0})
	}
	return data
}

// ----- logic / planning -----

type d6SATData struct {
	Vars    int     `json:"vars"`
	Clauses [][]int `json:"clauses"`
}

// d6SATData builds a satisfiable 3-SAT instance: pick a hidden satisfying
// assignment, then emit clauses each guaranteed true under it (every clause
// contains at least one literal that agrees with the hidden assignment).
// vars=14 keeps it non-trivial but the planted assignment keeps it satisfiable.
func d6GenSATData(r *modelBenchmarkRNG) d6SATData {
	vars := 18
	assignment := make([]bool, vars)
	for i := range assignment {
		assignment[i] = r.int(0, 1) == 1
	}
	clauseCount := 72
	clauses := make([][]int, 0, clauseCount)
	for c := 0; c < clauseCount; c++ {
		// Choose 3 distinct variables.
		idx := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}
		benchmarkShuffle(r, idx)
		chosen := idx[:3]
		clause := make([]int, 3)
		// Force the first literal to be satisfied by the hidden assignment;
		// the other two are random.
		for pos := 0; pos < 3; pos++ {
			v := chosen[pos]
			var lit int
			if pos == 0 {
				// literal that is TRUE under assignment[v]
				if assignment[v] {
					lit = v + 1
				} else {
					lit = -(v + 1)
				}
			} else {
				if r.int(0, 1) == 1 {
					lit = v + 1
				} else {
					lit = -(v + 1)
				}
			}
			clause[pos] = lit
		}
		clauses = append(clauses, clause)
	}
	return d6SATData{Vars: vars, Clauses: clauses}
}

// d6SATReference returns a satisfying assignment as the expected reference.
// The planted assignment is not recoverable from the data block alone, so we
// re-solve the instance deterministically by search and return that witness.
func d6SATReference(data d6SATData) []bool {
	assignment, _ := d6SolveSAT(data)
	return assignment
}

// d6SolveSAT is a DPLL-style backtracking solver used to derive the reference
// witness from the public data block.
func d6SolveSAT(data d6SATData) ([]bool, bool) {
	assign := make([]int, data.Vars) // 0 unassigned, 1 true, -1 false
	var satisfied func() bool
	satisfied = func() bool {
		for _, clause := range data.Clauses {
			clauseSat := false
			allAssigned := true
			for _, lit := range clause {
				v := lit
				if v < 0 {
					v = -v
				}
				idx := v - 1
				if assign[idx] == 0 {
					allAssigned = false
					continue
				}
				val := assign[idx] == 1
				if lit < 0 {
					val = !val
				}
				if val {
					clauseSat = true
					break
				}
			}
			if clauseSat {
				continue
			}
			if allAssigned {
				return false
			}
			// unresolved clause: keep searching
			return false // conservative: not fully satisfied yet
		}
		return true
	}
	var solve func() bool
	solve = func() bool {
		// Find first unassigned.
		varTarget := -1
		for i := range assign {
			if assign[i] == 0 {
				varTarget = i
				break
			}
		}
		if varTarget == -1 {
			return satisfied()
		}
		for _, val := range []int{1, -1} {
			assign[varTarget] = val
			if partialConsistent(data, assign) && solve() {
				return true
			}
			assign[varTarget] = 0
		}
		return false
	}
	if solve() {
		out := make([]bool, data.Vars)
		for i, v := range assign {
			out[i] = v == 1
		}
		return out, true
	}
	return nil, false
}

// partialConsistent reports whether any clause is already falsified.
func partialConsistent(data d6SATData, assign []int) bool {
	for _, clause := range data.Clauses {
		sat := false
		open := false
		for _, lit := range clause {
			v := lit
			if v < 0 {
				v = -v
			}
			idx := v - 1
			if assign[idx] == 0 {
				open = true
				continue
			}
			val := assign[idx] == 1
			if lit < 0 {
				val = !val
			}
			if val {
				sat = true
				break
			}
		}
		if !sat && !open {
			return false
		}
	}
	return true
}

// d6SATCheck verifies a candidate assignment satisfies every clause (witness).
func d6SATCheck(data d6SATData, assignment []bool) bool {
	if len(assignment) != data.Vars {
		return false
	}
	for _, clause := range data.Clauses {
		sat := false
		for _, lit := range clause {
			v := lit
			neg := v < 0
			if neg {
				v = -v
			}
			idx := v - 1
			if idx < 0 || idx >= len(assignment) {
				return false
			}
			val := assignment[idx]
			if neg {
				val = !val
			}
			if val {
				sat = true
				break
			}
		}
		if !sat {
			return false
		}
	}
	return true
}

// d6EpistemicData builds a 5-agent public-announcement puzzle. We fix a hidden
// world, derive the public statements, and the answer is the unique set of
// agents who can deduce their own status.
type d6EpistemicData struct {
	Agents     int   `json:"agents"`
	SeenCounts []int `json:"seen_counts"`
}

// d6EpistemicAnswer: for a "mud-children" style variant, agents holding a key
// who see exactly k-1 other keys deduce on round k. We implement the classic
// fixed puzzle: everyone sees others' keys but not their own; the host announces
// "at least one key exists"; on each round anyone who can deduce speaks.
// Agents with keys deduce on round = (number of keys they see)+1. The answer is
// the set of agents who eventually know their own status.
func d6GenEpistemicData(r *modelBenchmarkRNG) d6EpistemicData {
	agents := r.int(6, 9)
	keys := make([]bool, agents)
	count := 0
	for count == 0 || count == agents {
		count = 0
		for i := range keys {
			keys[i] = r.int(0, 1) == 1
			if keys[i] {
				count++
			}
		}
	}
	seen := make([]int, agents)
	for i := range keys {
		if keys[i] {
			seen[i] = count - 1
		} else {
			seen[i] = count
		}
	}
	return d6EpistemicData{Agents: agents, SeenCounts: seen}
}

// d6EpistemicAnswer: in the classic puzzle, exactly the key holders deduce on
// round == total number of keys. The unique answer is the set of key holders.
func d6EpistemicAnswer(data d6EpistemicData) []int {
	total := 0
	for _, v := range data.SeenCounts {
		if v > total {
			total = v
		}
	}
	out := []int{}
	for i, v := range data.SeenCounts {
		if v == total-1 {
			out = append(out, i)
		}
	}
	return out
}

// d6RouteData scales route-with-windows to 8 stops.
type d6RouteData struct {
	Travel  [][]int  `json:"travel"`
	Windows [][2]int `json:"windows"`
	Service int      `json:"service"`
}

func d6GenRouteData(r *modelBenchmarkRNG) d6RouteData {
	const stops = 10
	n := stops + 1
	travel := make([][]int, n)
	for i := range travel {
		travel[i] = make([]int, n)
		for j := 0; j < i; j++ {
			travel[i][j] = r.int(2, 12)
			travel[j][i] = travel[i][j]
		}
	}
	windows := make([][2]int, 0, stops)
	for i := 0; i < stops; i++ {
		lo := r.int(0, 20)
		hi := lo + r.int(15, 60)
		windows = append(windows, [2]int{lo, hi})
	}
	return d6RouteData{Travel: travel, Windows: windows, Service: r.int(1, 4)}
}

// d6RouteFinish evaluates one visiting order under time windows.
func d6RouteFinish(data d6RouteData, order []int) (int, bool) {
	stops := len(data.Windows)
	if len(order) != stops {
		return 0, false
	}
	seen := map[int]bool{}
	node, clock := 0, 0
	for _, next := range order {
		if next < 1 || next > stops || seen[next] {
			return 0, false
		}
		seen[next] = true
		clock = max(clock+data.Travel[node][next], data.Windows[next-1][0])
		if clock > data.Windows[next-1][1] {
			return 0, false
		}
		clock += data.Service
		node = next
	}
	return clock, true
}

// d6BestRoute finds the optimal finish time and a witness order by DP over
// subsets (8 stops -> 2^8 * 8 states), tracking earliest arrival.
func d6BestRoute(data d6RouteData) map[string]any {
	stops := len(data.Windows)
	const inf = 1 << 30
	size := 1 << stops
	type cell struct {
		finish int
		order  []int
	}
	dp := make([][]int, size)
	for i := range dp {
		dp[i] = make([]int, stops)
		for j := range dp[i] {
			dp[i][j] = inf
		}
	}
	for s := 0; s < stops; s++ {
		arr := max(data.Travel[0][s+1], data.Windows[s][0])
		if arr <= data.Windows[s][1] {
			dp[1<<s][s] = arr + data.Service
		}
	}
	for mask := 0; mask < size; mask++ {
		for last := 0; last < stops; last++ {
			cur := dp[mask][last]
			if cur == inf {
				continue
			}
			for nxt := 0; nxt < stops; nxt++ {
				if mask>>nxt&1 == 1 {
					continue
				}
				// cur already includes service time at last; travel to nxt.
				arr := cur + data.Travel[last+1][nxt+1]
				if arr < data.Windows[nxt][0] {
					arr = data.Windows[nxt][0]
				}
				if arr > data.Windows[nxt][1] {
					continue
				}
				fin := arr + data.Service
				if fin < dp[mask|1<<nxt][nxt] {
					dp[mask|1<<nxt][nxt] = fin
				}
			}
		}
	}
	full := size - 1
	best, bestLast := inf, -1
	for last := 0; last < stops; last++ {
		if dp[full][last] < best {
			best, bestLast = dp[full][last], last
		}
	}
	if bestLast < 0 {
		return map[string]any{"order": []int{}, "finish_time": -1}
	}
	// Reconstruct one optimal order by backtracking.
	order := make([]int, 0, stops)
	mask, last := full, bestLast
	for mask != 0 {
		order = append([]int{last + 1}, order...)
		prevMask := mask &^ (1 << last)
		for prev := 0; prev < stops; prev++ {
			if prevMask>>prev&1 == 0 {
				continue
			}
			pf := dp[prevMask][prev]
			if pf == inf {
				continue
			}
			arr := pf + data.Travel[prev+1][last+1]
			if arr < data.Windows[last][0] {
				arr = data.Windows[last][0]
			}
			if arr <= data.Windows[last][1] && arr+data.Service == dp[mask][last] {
				mask, last = prevMask, prev
				break
			}
		}
		if prevMask == 0 {
			break
		}
	}
	return map[string]any{"order": order, "finish_time": best}
}

// d6ResourceData scales the knapsack to 25 items and 5 capacity dimensions.
type d6ResourceItem struct {
	Costs [5]int `json:"costs"`
	Value int    `json:"value"`
}
type d6ResourceData struct {
	Items      []d6ResourceItem `json:"items"`
	Capacities [5]int           `json:"capacities"`
}

func d6GenResourceData(r *modelBenchmarkRNG) d6ResourceData {
	items := make([]d6ResourceItem, 0, 24)
	for i := 0; i < 24; i++ {
		var costs [5]int
		for d := 0; d < 5; d++ {
			costs[d] = r.int(1, 9)
		}
		items = append(items, d6ResourceItem{Costs: costs, Value: r.int(2, 30)})
	}
	var caps [5]int
	for d := 0; d < 5; d++ {
		caps[d] = r.int(14, 24)
	}
	return d6ResourceData{Items: items, Capacities: caps}
}

// d6BestResource solves the 0/1 multi-dimensional knapsack by DFS with a
// remaining-value upper bound for branch-and-bound pruning. Returns one optimal
// subset (ascending original indices) as the reference; the witness checker
// accepts any subset reaching the optimal value.
func d6BestResource(data d6ResourceData) []int {
	best := -1
	var bestSet []int
	n := len(data.Items)
	// suffixValue[i] = sum of values of items i..n-1, for the upper bound.
	suffixValue := make([]int, n+1)
	for i := n - 1; i >= 0; i-- {
		suffixValue[i] = suffixValue[i+1] + data.Items[i].Value
	}
	var dfs func(i int, caps [5]int, value int, picked []int)
	dfs = func(i int, caps [5]int, value int, picked []int) {
		if value > best {
			best = value
			bestSet = append([]int{}, picked...)
		}
		if i == n {
			return
		}
		// Branch and bound: even taking everything left cannot beat best.
		if value+suffixValue[i] <= best {
			return
		}
		// take item i if feasible (try taking first for faster convergence)
		it := data.Items[i]
		ok := true
		var nc [5]int
		for d := 0; d < 5; d++ {
			nc[d] = caps[d] - it.Costs[d]
			if nc[d] < 0 {
				ok = false
				break
			}
		}
		if ok {
			dfs(i+1, nc, value+it.Value, append(picked, i))
		}
		// skip item i
		dfs(i+1, caps, value, picked)
	}
	dfs(0, data.Capacities, 0, nil)
	sort.Ints(bestSet)
	return bestSet
}

// d6MatrixChain builds a matrix-chain instance with n matrices (dims n+1).
func d6GenMatrixChain(r *modelBenchmarkRNG) map[string]any {
	n := 10
	dims := make([]int, n+1)
	for i := range dims {
		dims[i] = r.int(2, 30)
	}
	return map[string]any{"dims": dims}
}

// d6MatrixChain solves the classic interval DP for minimum scalar products.
func d6MatrixChain(data map[string]any) int {
	raw := data["dims"].([]int)
	n := len(raw) - 1
	const inf = int64(1) << 60
	dp := make([][]int64, n)
	for i := range dp {
		dp[i] = make([]int64, n)
	}
	for length := 2; length <= n; length++ {
		for i := 0; i+length-1 < n; i++ {
			j := i + length - 1
			dp[i][j] = inf
			for k := i; k < j; k++ {
				c := dp[i][k] + dp[k+1][j] + int64(raw[i])*int64(raw[k+1])*int64(raw[j+1])
				if c < dp[i][j] {
					dp[i][j] = c
				}
			}
		}
	}
	return int(dp[0][n-1])
}

// d6Schedule simulates list-scheduling: ready tasks start ASAP; ties broken by
// ascending index; machines limited. Returns makespan.

// d6NimData builds a Nim position with 4-6 heaps.
type d6NimData struct {
	Heaps []int `json:"heaps"`
}

func d6GenNimData(r *modelBenchmarkRNG) d6NimData {
	heaps := benchmarkCodingInts(r, r.int(4, 6), 3, 31)
	return d6NimData{Heaps: heaps}
}

// d6Nim returns a winning first move [heap_index, remove_count], or [] if losing.
func d6Nim(data d6NimData) []int {
	x := 0
	for _, h := range data.Heaps {
		x ^= h
	}
	if x == 0 {
		return []int{}
	}
	for i, h := range data.Heaps {
		if (h ^ x) < h {
			return []int{i, h - (h ^ x)}
		}
	}
	return []int{}
}
