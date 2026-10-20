package service

import (
	"fmt"
	"sort"
)

// These tasks require complete execution traces, fixed points, semantic repairs
// or optimal answers. All state and language rules are visible in the prompt.
var modelBenchmarkCodingFamilies = []modelBenchmarkFamilySpec{
	{"slice-alias-trace", "code", 5, "exact"},
	{"defer-unwind-trace", "code", 5, "exact"},
	{"event-loop-trace", "code", 5, "sequence"},
	{"snapshot-write-skew", "code", 5, "exact"},
	{"serializability-graph", "code", 5, "exact"},
	{"cas-interleavings", "code", 5, "exact"},
	{"lock-cycle-members", "code", 5, "exact"},
	{"lru-writeback", "code", 5, "exact"},
	{"taint-dataflow", "code", 5, "sequence"},
	{"bytecode-control-flow", "code", 5, "exact"},
	{"signed-overflow-trace", "code", 5, "sequence"},
	{"struct-layout-padding", "code", 5, "exact"},
	{"repair-lower-bound", "code", 5, "coding_patch"},
	{"repair-range-sum", "code", 5, "coding_patch"},
	{"minimal-bug-witness", "code", 5, "coding_witness"},
	{"sql-null-antijoin", "code", 5, "exact"},
	{"negative-cycle-influence", "algorithms", 5, "sequence"},
	{"maxflow-mincut", "algorithms", 5, "exact"},
	{"bipartite-matching", "algorithms", 5, "exact"},
	{"scc-condensation", "algorithms", 5, "exact"},
	{"weighted-edit-distance", "algorithms", 5, "integer"},
	{"lis-count", "algorithms", 5, "exact"},
	{"range-update-query", "algorithms", 5, "sequence"},
	{"bitmask-shortest-path", "algorithms", 5, "integer"},
}

func benchmarkCodingInts(r *modelBenchmarkRNG, n, lo, hi int) []int {
	values := make([]int, n)
	for i := range values {
		values[i] = r.int(lo, hi)
	}
	return values
}

func generateModelBenchmarkCoding(f modelBenchmarkFamilySpec, r *modelBenchmarkRNG, _ int) (string, any) {
	switch f.id {
	case "slice-alias-trace":
		values := benchmarkCodingInts(r, 10, -20, 40)
		limit, x, y, delta, sub, z := 4+2*r.int(0, 1), r.int(21, 40), r.int(41, 60), r.int(2, 9), r.int(1, 8), r.int(61, 80)
		base := append([]int{}, values...)
		a := base[1:4:limit]
		b := a[:2]
		c := append(a, x, y)
		b[1] += delta
		c[0] -= sub
		b = append(b, z)
		data := map[string]any{"values": values, "limit": limit, "x": x, "y": y, "delta": delta, "sub": sub, "z": z}
		return modelBenchmarkDataQuestion("按 Go 切片语义执行：base=values 的独立副本；a:=base[1:4:limit]; b:=a[:2]; c:=append(a,x,y); b[1]+=delta; c[0]-=sub; b=append(b,z)。append 容量足够时复用原数组，不足时分配并复制。返回 {base:[...],a:[...],b:[...],c:[...]} 的最终可见元素。", data), map[string]any{"base": base, "a": a, "b": b, "c": c}
	case "defer-unwind-trace":
		a, b, c, d, e := r.int(2, 8), r.int(2, 6), r.int(1, 7), r.int(2, 4), r.int(-3, 9)
		log := []int{}
		run := func() (out int) {
			out = a
			defer func() { out += c; log = append(log, out) }()
			defer func(v int) { out += v; log = append(log, out) }(out)
			out *= b
			defer func() { out *= d; log = append(log, out) }()
			return out + e
		}
		value := run()
		return modelBenchmarkDataQuestion("Go 代码 f() (out int) { out=a; defer func(){out+=c; log=append(log,out)}(); defer func(v int){out+=v; log=append(log,out)}(out); out*=b; defer func(){out*=d; log=append(log,out)}(); return out+e }。log 初始为空。按命名返回值、defer 实参注册时求值及 LIFO 规则，返回 {result: f()返回值, log:[执行日志]}。", map[string]int{"a": a, "b": b, "c": c, "d": d, "e": e}), map[string]any{"result": value, "log": log}
	case "event-loop-trace":
		counter := r.int(100, 900)
		var task func(int) benchmarkEventTask
		task = func(depth int) benchmarkEventTask {
			counter++
			t := benchmarkEventTask{Label: fmt.Sprintf("e%d", counter), Enqueue: []benchmarkEventTask{}}
			if depth > 0 {
				for i, n := 0, r.int(1, 2); i < n; i++ {
					t.Enqueue = append(t.Enqueue, task(depth-1))
				}
			}
			return t
		}
		data := benchmarkEventData{Sync: []benchmarkEventTask{task(2), task(1)}, Timers: []benchmarkEventTask{task(2), task(1), task(1)}}
		return modelBenchmarkDataQuestion("事件循环：执行一个任务时先记录 label，再将 enqueue 列表按顺序追加到微任务队尾。先顺序执行全部 sync 任务，再清空微任务队列（包括新追加的微任务）；随后按 timers 顺序每执行一个计时器，就再次清空微任务队列。任务不会新增计时器。返回完整 label 日志数组。", data), benchmarkEventLog(data)
	case "snapshot-write-skew":
		data := benchmarkSnapshotData{Initial: benchmarkCodingInts(r, 4, 3, 12), Minimum: r.int(6, 15), Targets: benchmarkCodingInts(r, 5, 0, 3), Order: []int{0, 1, 2, 3, 4}}
		benchmarkShuffle(r, data.Order)
		return modelBenchmarkDataQuestion("所有事务在同一 initial 快照开始。事务 i 仅当快照中除 targets[i] 外的余额总和 >= minimum 才申请把该下标余额置零；否则提交只读事务。按 order 提交，采用 first-committer-wins：写目标若已被先提交的写事务修改则中止；只读事务总能提交。返回 {committed:[按提交顺序的成功事务编号],balances:[最终余额],invariant:最终余额总和是否>=minimum}。", data), benchmarkSnapshot(data)
	case "serializability-graph":
		data := benchmarkConflictData{Transactions: 5, Operations: []benchmarkDBOperation{}}
		for i := 0; i < 36; i++ {
			data.Operations = append(data.Operations, benchmarkDBOperation{Tx: r.int(0, 4), Key: r.int(0, 3), Write: r.int(0, 1) == 1})
		}
		return modelBenchmarkDataQuestion("operations 是从早到晚的数据库操作，tx 为事务编号，key 为数据项，write=false 表示读。不同事务操作同一 key 且至少一个写时，给先操作的事务到后操作的事务连依赖边。返回 {serializable:冲突图是否无环,cycle_members:[属于至少一个有向环的全部事务编号，升序]}。同一事务的操作不连自环。", data), benchmarkConflictGraph(data)
	case "cas-interleavings":
		data := map[string]int{"threads": r.int(2, 3), "increments": 1, "initial": r.int(0, 9)}
		if data["threads"] == 2 {
			data["increments"] = 2
		}
		return modelBenchmarkDataQuestion("每线程执行 increments 次原子递增：每轮先做一个原子 read 保存 local，再做一个原子 CAS(shared,local,local+1)；CAS 成功进入下一轮 read，失败增加失败计数并回到同轮 read。shared 初始 initial。调度每步选择一个未结束线程执行其下一原子操作；不同线程编号序列视为不同调度，结束后无空操作。枚举所有合法完整调度，返回 {final:最终shared,distribution:[[CAS失败总数,对应调度数量],...]}，按失败数升序。", data), benchmarkCASDistribution(data["threads"], data["increments"], data["initial"])
	case "lock-cycle-members":
		data := benchmarkLockData{Owner: []int{0, 1, 2, 3, 4, 5, 6, 7}, Waiting: benchmarkCodingInts(r, 8, -1, 7)}
		benchmarkShuffle(r, data.Owner)
		return modelBenchmarkDataQuestion("owner[k] 是持有锁 k 的线程，waiting[t] 是线程 t 正在申请的锁，-1 表示未等待。锁为非可重入独占锁；线程等待时不释放已持有锁，其他线程不会替它释放。返回 {cycle_members:[等待图中环成员，含自等待],blocked:[环成员及沿等待链最终进入环的线程]}，两数组均升序。无等待且无环的链最终可完成。", data), benchmarkLockCycles(data)
	case "lru-writeback":
		data := benchmarkLRUData{Capacity: r.int(2, 4), Operations: []benchmarkCacheOperation{}}
		for i := 0; i < 36; i++ {
			data.Operations = append(data.Operations, benchmarkCacheOperation{Key: r.int(0, 6), Write: r.int(0, 2) == 0})
		}
		return modelBenchmarkDataQuestion("空缓存按 operations 访问。命中移到 MRU；缺页先按 LRU 淘汰（满时），再载入并移到 MRU。write=true 将页标脏，读不会清除脏标记；淘汰脏页计一次写回，结束时不额外刷盘。返回 {misses:缺页数,writebacks:写回数,order:[最终LRU到MRU的key],dirty:[最终脏页key，升序]}。", data), benchmarkLRU(data)
	case "taint-dataflow":
		data := []benchmarkTaintNode{}
		for i := 0; i < 12; i++ {
			parents := []int{r.int(0, 7), r.int(0, 7)}
			data = append(data, benchmarkTaintNode{Sources: r.int(0, 15), Kill: r.int(0, 15), Parents: parents})
		}
		return modelBenchmarkDataQuestion("4 种污点由位 1/2/4/8 表示。节点 i 的方程是 T[i]=(sources[i] OR 所有parents的T) AND (15 XOR kill[i])。图允许环和自环，取从全零开始的最小不动点。返回按节点编号排列的 T 整数数组，不能选择另一个更大的不动点。", data), benchmarkTaint(data)
	case "bytecode-control-flow":
		data := benchmarkBytecodeData{Locals: []int{r.int(5, 10), r.int(1, 9), r.int(2, 8), r.int(-5, 5)}, Program: []benchmarkInstruction{
			{"load", 0}, {"jz", 23}, {"load", 0}, {"push", 2}, {"mod", 0}, {"jz", 14},
			{"load", 3}, {"load", 1}, {"add", 0}, {"store", 3}, {"load", 1}, {"load", 2}, {"add", 0}, {"store", 1},
			{"load", 2}, {"load", 1}, {"xor", 0}, {"store", 2}, {"load", 0}, {"push", 1}, {"sub", 0}, {"store", 0}, {"jmp", 0}, {"halt", 0},
		}}
		return modelBenchmarkDataQuestion("栈式虚拟机：PC=0、栈初始为空。load i 压入locals[i]；push n 压常数；store i 弹栈赋值；二元 op 先弹 b 后弹 a，压 a op b（mod 为整数余数，xor 为位异或）。jz 弹栈，若为0跳到arg，否则PC+1；jmp 无条件跳；halt 停机。每执行一条指令计1步，含halt；其余指令PC+1。整数无限精度且本题不溢出。返回 {locals:[最终局部变量],steps:步数}。", data), benchmarkBytecode(data)
	case "signed-overflow-trace":
		data := benchmarkOverflowData{Bits: r.int(6, 9), Initial: r.int(-24, 24), Operations: []benchmarkInstruction{}}
		ops := []string{"add", "mul", "xor", "shr", "ushr"}
		for i := 0; i < 28; i++ {
			op := ops[r.int(0, len(ops)-1)]
			arg := r.int(2, 7)
			if op == "shr" || op == "ushr" {
				arg = r.int(1, 3)
			}
			data.Operations = append(data.Operations, benchmarkInstruction{op, arg})
		}
		return modelBenchmarkDataQuestion("按 bits 位二进制补码寄存器执行。add/mul/xor 对arg运算后仅保留低bits位；shr 为符号扩展的算术右移，ushr 为把寄存器视为无符号数的逻辑右移。每步均将结果解释成 bits 位有符号整数。返回每次操作后的值数组（不含initial）。", data), benchmarkOverflow(data)
	case "struct-layout-padding":
		fields := []benchmarkLayoutField{}
		for i := 0; i < 12; i++ {
			fields = append(fields, benchmarkLayoutField{Size: 1 << r.int(0, 3), Count: r.int(1, 3)})
		}
		return modelBenchmarkDataQuestion("明示 ABI：每个字段是 count 个 size 字节元素的定长数组，字段总大小=size*count、对齐=size；字段按给定顺序布局，放在不小于前一字段末尾的最小对齐偏移。结构体对齐为所有字段对齐的最大值，最终大小向上对齐到结构体对齐。不允许重排或压缩。返回 {offsets:[字段偏移],size:结构体字节数,padding:所有内部和尾部填充总字节数}。", fields), benchmarkLayout(fields)
	case "repair-lower-bound":
		data := benchmarkLowerBoundData{Descending: r.int(0, 1) == 1, Strict: r.int(0, 1) == 1, Minimum: -2, Maximum: 2, MaxLength: 5}
		cmp := "lt"
		if data.Strict {
			cmp = "le"
		}
		if data.Descending {
			cmp = "gt"
			if data.Strict {
				cmp = "ge"
			}
		}
		return modelBenchmarkDataQuestion("修复二分程序：lo=0; hi=len(a); while lo<hi: mid=floor((lo+hi)/2); if CMP(a[mid],target): lo=mid+advance; else: hi=mid+retreat; return lo+result_offset。a 按 descending 指定方向有序；升序要找第一个 >=target（strict=true时为>）；降序找第一个 <=target（strict=true时为<）；不存在返回len(a)。返回补丁对象 {comparison:字符串,advance:整数,retreat:整数,result_offset:整数}。comparison 仅 lt/le/gt/ge；advance 只能0/1，另两数只能-1/0/1。补丁须对元素在minimum..maximum、长度0..max_length的所有有序数组及target在minimum-1..maximum+1上正确终止，且不能越界。", data), map[string]any{"comparison": cmp, "advance": 1, "retreat": 0, "result_offset": 0}
	case "repair-range-sum":
		data := benchmarkRangePatchData{IndexBase: r.int(0, 1), IncludeEnd: r.int(0, 1) == 1, MaxLength: 4}
		end := -data.IndexBase
		if data.IncludeEnd {
			end++
		}
		return modelBenchmarkDataQuestion("修复循环数组区间和公式。P[0]=0，P[i+1]=P[i]+a[i]。start/end 采用index_base下标，沿下标递增方向从start走到end，include_end=true含end，否则不含end；start=end时分别为单元素或空区间，不绕整圈。候选公式：P[end+end_offset]-P[start+start_offset]+(CMP(end,start)?wrap_factor*P[n]:0)。返回 {start_offset:整数,end_offset:整数,wrap_comparison:字符串,wrap_factor:整数}。三个整数仅-1/0/1，比较仅lt/le/gt/ge；须在长度1..max_length、元素均为-1/0/1的所有数组和全部合法start/end上正确，前缀下标必须始终在0..n。", data), map[string]any{"start_offset": -data.IndexBase, "end_offset": end, "wrap_comparison": "lt", "wrap_factor": 1}
	case "minimal-bug-witness":
		positive := r.int(2, 4)
		data := benchmarkBugData{Minimum: -r.int(1, 3), Maximum: positive, Target: r.int(positive+1, 2*positive), MaxLength: 4}
		return modelBenchmarkDataQuestion("为错误滑动窗口程序构造最短反例。程序：left=0; sum=0; best=len(a)+1; for right=0..len(a)-1: sum+=a[right]; while sum>=target: best=min(best,right-left+1); sum-=a[left]; left++; 返回best（若仍>len(a)则返回0）。正确规格是所有非空连续子数组中sum>=target的最短长度，无解返回0。返回整数数组a，元素minimum..maximum，长度1..max_length，使两者结果不同，且长度在此输入域中最小；同长度任意反例均接受。", data), benchmarkFindBugWitness(data)
	case "sql-null-antijoin":
		data := benchmarkSQLData{Left: []benchmarkNullableRow{}, Right: []benchmarkNullableRow{}}
		for i := 0; i < 12; i++ {
			data.Left = append(data.Left, benchmarkNullableRow{ID: i, Key: r.int(0, 4), Null: r.int(0, 3) == 0, Active: true})
		}
		for i := 0; i < 8; i++ {
			data.Right = append(data.Right, benchmarkNullableRow{ID: i, Key: r.int(0, 4), Null: r.int(0, 2) == 0, Active: r.int(0, 2) != 0})
		}
		return modelBenchmarkDataQuestion("null=true表示SQL NULL，key数值此时忽略。仅用right中active=true的行。分别执行：SELECT l.id FROM left l WHERE l.key NOT IN (SELECT r.key FROM right r WHERE r.active)；以及 SELECT l.id FROM left l WHERE NOT EXISTS (SELECT 1 FROM right r WHERE r.active AND r.key=l.key)。SQL采用三值逻辑，WHERE只保留TRUE；NULL等号结果UNKNOWN；空集合的NOT IN为TRUE（含左值NULL）。返回 {not_in:[升序id],not_exists:[升序id]}。", data), benchmarkSQLAntiJoin(data)
	default:
		return generateModelBenchmarkCodingAlgorithms(f, r)
	}
}

type benchmarkEventTask struct {
	Label   string               `json:"label"`
	Enqueue []benchmarkEventTask `json:"enqueue"`
}
type benchmarkEventData struct {
	Sync   []benchmarkEventTask `json:"sync"`
	Timers []benchmarkEventTask `json:"timers"`
}

func benchmarkEventLog(data benchmarkEventData) []string {
	log, queue := []string{}, []benchmarkEventTask{}
	run := func(task benchmarkEventTask) { log = append(log, task.Label); queue = append(queue, task.Enqueue...) }
	drain := func() {
		for len(queue) > 0 {
			task := queue[0]
			queue = queue[1:]
			run(task)
		}
	}
	for _, task := range data.Sync {
		run(task)
	}
	drain()
	for _, task := range data.Timers {
		run(task)
		drain()
	}
	return log
}

type benchmarkSnapshotData struct {
	Initial []int `json:"initial"`
	Minimum int   `json:"minimum"`
	Targets []int `json:"targets"`
	Order   []int `json:"order"`
}

func benchmarkSnapshot(data benchmarkSnapshotData) map[string]any {
	balances, committed := append([]int{}, data.Initial...), []int{}
	modified := map[int]bool{}
	total := 0
	for _, value := range data.Initial {
		total += value
	}
	for _, tx := range data.Order {
		target := data.Targets[tx]
		write := total-data.Initial[target] >= data.Minimum
		if write && modified[target] {
			continue
		}
		committed = append(committed, tx)
		if write {
			modified[target], balances[target] = true, 0
		}
	}
	final := 0
	for _, value := range balances {
		final += value
	}
	return map[string]any{"committed": committed, "balances": balances, "invariant": final >= data.Minimum}
}

type benchmarkDBOperation struct {
	Tx    int  `json:"tx"`
	Key   int  `json:"key"`
	Write bool `json:"write"`
}
type benchmarkConflictData struct {
	Transactions int                    `json:"transactions"`
	Operations   []benchmarkDBOperation `json:"operations"`
}

func benchmarkReachability(n int, edges [][2]int) [][]bool {
	reach := make([][]bool, n)
	for i := range reach {
		reach[i] = make([]bool, n)
	}
	for _, edge := range edges {
		reach[edge[0]][edge[1]] = true
	}
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				reach[i][j] = reach[i][j] || reach[i][k] && reach[k][j]
			}
		}
	}
	return reach
}

func benchmarkConflictGraph(data benchmarkConflictData) map[string]any {
	edges := [][2]int{}
	for i, a := range data.Operations {
		for _, b := range data.Operations[i+1:] {
			if a.Tx != b.Tx && a.Key == b.Key && (a.Write || b.Write) {
				edges = append(edges, [2]int{a.Tx, b.Tx})
			}
		}
	}
	reach, members := benchmarkReachability(data.Transactions, edges), []int{}
	for i := range reach {
		if reach[i][i] {
			members = append(members, i)
		}
	}
	return map[string]any{"serializable": len(members) == 0, "cycle_members": members}
}

func benchmarkCASDistribution(threads, increments, initial int) map[string]any {
	remaining, phase, local := make([]int, threads), make([]int, threads), make([]int, threads)
	for i := range remaining {
		remaining[i] = increments
	}
	counts := map[int]int{}
	var enumerate func(int, int)
	enumerate = func(shared, failures int) {
		active := false
		for t := 0; t < threads; t++ {
			if remaining[t] == 0 {
				continue
			}
			active = true
			oldPhase, oldLocal, oldRemaining := phase[t], local[t], remaining[t]
			if phase[t] == 0 {
				phase[t], local[t] = 1, shared
				enumerate(shared, failures)
			} else {
				phase[t] = 0
				if local[t] == shared {
					remaining[t]--
					enumerate(shared+1, failures)
				} else {
					enumerate(shared, failures+1)
				}
			}
			phase[t], local[t], remaining[t] = oldPhase, oldLocal, oldRemaining
		}
		if !active {
			counts[failures]++
		}
	}
	enumerate(initial, 0)
	distribution := [][]int{}
	for _, failures := range benchmarkSortedKeys(counts) {
		distribution = append(distribution, []int{failures, counts[failures]})
	}
	return map[string]any{"final": initial + threads*increments, "distribution": distribution}
}

type benchmarkLockData struct {
	Owner   []int `json:"owner"`
	Waiting []int `json:"waiting"`
}

func benchmarkLockCycles(data benchmarkLockData) map[string]any {
	edges := [][2]int{}
	for t, lock := range data.Waiting {
		if lock >= 0 {
			edges = append(edges, [2]int{t, data.Owner[lock]})
		}
	}
	reach := benchmarkReachability(len(data.Waiting), edges)
	cycles, blocked := []int{}, []int{}
	for i := range reach {
		if reach[i][i] {
			cycles = append(cycles, i)
		}
		for j := range reach {
			if reach[j][j] && reach[i][j] {
				blocked = append(blocked, i)
				break
			}
		}
	}
	return map[string]any{"cycle_members": cycles, "blocked": blocked}
}

type benchmarkCacheOperation struct {
	Key   int  `json:"key"`
	Write bool `json:"write"`
}
type benchmarkLRUData struct {
	Capacity   int                       `json:"capacity"`
	Operations []benchmarkCacheOperation `json:"operations"`
}

func benchmarkLRU(data benchmarkLRUData) map[string]any {
	order, dirty := []int{}, map[int]bool{}
	misses, writes := 0, 0
	for _, op := range data.Operations {
		index := -1
		for i, key := range order {
			if key == op.Key {
				index = i
				break
			}
		}
		if index >= 0 {
			order = append(order[:index], order[index+1:]...)
		} else {
			misses++
			if len(order) == data.Capacity {
				if dirty[order[0]] {
					writes++
				}
				delete(dirty, order[0])
				order = order[1:]
			}
		}
		order = append(order, op.Key)
		dirty[op.Key] = dirty[op.Key] || op.Write
	}
	dirtyKeys := []int{}
	for key, value := range dirty {
		if value {
			dirtyKeys = append(dirtyKeys, key)
		}
	}
	sort.Ints(dirtyKeys)
	return map[string]any{"misses": misses, "writebacks": writes, "order": order, "dirty": dirtyKeys}
}

type benchmarkTaintNode struct {
	Sources int   `json:"sources"`
	Kill    int   `json:"kill"`
	Parents []int `json:"parents"`
}

func benchmarkTaint(nodes []benchmarkTaintNode) []int {
	values := make([]int, len(nodes))
	for changed := true; changed; {
		changed = false
		for i, node := range nodes {
			value := node.Sources
			for _, parent := range node.Parents {
				value |= values[parent]
			}
			value &= 15 ^ node.Kill
			changed = changed || values[i] != value
			values[i] = value
		}
	}
	return values
}

type benchmarkInstruction struct {
	Op  string `json:"op"`
	Arg int    `json:"arg"`
}
type benchmarkBytecodeData struct {
	Locals  []int                  `json:"locals"`
	Program []benchmarkInstruction `json:"program"`
}

func benchmarkBytecode(data benchmarkBytecodeData) map[string]any {
	locals, stack := append([]int{}, data.Locals...), []int{}
	steps := 0
	pop := func() int { value := stack[len(stack)-1]; stack = stack[:len(stack)-1]; return value }
	for pc := 0; ; {
		instruction := data.Program[pc]
		steps++
		pc++
		switch instruction.Op {
		case "load":
			stack = append(stack, locals[instruction.Arg])
		case "push":
			stack = append(stack, instruction.Arg)
		case "store":
			locals[instruction.Arg] = pop()
		case "add", "sub", "mod", "xor":
			b, a := pop(), pop()
			value := a + b
			switch instruction.Op {
			case "sub":
				value = a - b
			case "mod":
				value = a % b
			case "xor":
				value = a ^ b
			}
			stack = append(stack, value)
		case "jz":
			if pop() == 0 {
				pc = instruction.Arg
			}
		case "jmp":
			pc = instruction.Arg
		case "halt":
			return map[string]any{"locals": locals, "steps": steps}
		}
	}
}

type benchmarkOverflowData struct {
	Bits       int                    `json:"bits"`
	Initial    int                    `json:"initial"`
	Operations []benchmarkInstruction `json:"operations"`
}

func benchmarkOverflow(data benchmarkOverflowData) []int {
	mask, value, out := (1<<data.Bits)-1, data.Initial, []int{}
	for _, op := range data.Operations {
		switch op.Op {
		case "add":
			value += op.Arg
		case "mul":
			value *= op.Arg
		case "xor":
			value ^= op.Arg
		case "shr":
			value >>= op.Arg
		case "ushr":
			value = (value & mask) >> op.Arg
		}
		value &= mask
		if value>>(data.Bits-1) != 0 {
			value -= mask + 1
		}
		out = append(out, value)
	}
	return out
}

type benchmarkLayoutField struct {
	Size  int `json:"size"`
	Count int `json:"count"`
}

func benchmarkLayout(fields []benchmarkLayoutField) map[string]any {
	offsets, end, alignment, content := []int{}, 0, 1, 0
	for _, field := range fields {
		end = (end + field.Size - 1) / field.Size * field.Size
		offsets = append(offsets, end)
		end += field.Size * field.Count
		content += field.Size * field.Count
		alignment = max(alignment, field.Size)
	}
	size := (end + alignment - 1) / alignment * alignment
	return map[string]any{"offsets": offsets, "size": size, "padding": size - content}
}

type benchmarkNullableRow struct {
	ID     int  `json:"id"`
	Key    int  `json:"key"`
	Null   bool `json:"null"`
	Active bool `json:"active"`
}
type benchmarkSQLData struct {
	Left  []benchmarkNullableRow `json:"left"`
	Right []benchmarkNullableRow `json:"right"`
}

func benchmarkSQLAntiJoin(data benchmarkSQLData) map[string]any {
	notIn, notExists := []int{}, []int{}
	for _, left := range data.Left {
		match, unknown, count := false, false, 0
		for _, right := range data.Right {
			if right.Active {
				count++
				unknown = unknown || left.Null || right.Null
				match = match || !left.Null && !right.Null && left.Key == right.Key
			}
		}
		if !match {
			notExists = append(notExists, left.ID)
			if count == 0 || !unknown {
				notIn = append(notIn, left.ID)
			}
		}
	}
	sort.Ints(notIn)
	sort.Ints(notExists)
	return map[string]any{"not_in": notIn, "not_exists": notExists}
}
