package service

import (
	"sort"
)

type benchmarkCodingEdge struct {
	From int `json:"from"`
	To   int `json:"to"`
	Cost int `json:"cost"`
	Need int `json:"need"`
}
type benchmarkCodingGraph struct {
	Nodes int                   `json:"nodes"`
	Edges []benchmarkCodingEdge `json:"edges"`
}

func benchmarkRandomCodingGraph(r *modelBenchmarkRNG, n, low, high int) benchmarkCodingGraph {
	data := benchmarkCodingGraph{Nodes: n, Edges: []benchmarkCodingEdge{}}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j && r.int(0, 3) == 0 {
				data.Edges = append(data.Edges, benchmarkCodingEdge{From: i, To: j, Cost: r.int(low, high)})
			}
		}
	}
	return data
}

func generateModelBenchmarkCodingAlgorithms(f modelBenchmarkFamilySpec, r *modelBenchmarkRNG) (string, any) {
	switch f.id {
	case "negative-cycle-influence":
		data := benchmarkRandomCodingGraph(r, 7, -4, 9)
		return modelBenchmarkDataQuestion("有向图节点0..nodes-1，从0出发，边权cost可为负，need字段忽略。返回逐节点最短距离数组：不可达用字符串\"unreachable\"；从0可达的负环能够进一步到达该节点则用字符串\"-inf\"；其他节点用最短路整数。0到自身允许空路径。", data), benchmarkNegativePaths(data)
	case "maxflow-mincut":
		data := benchmarkRandomCodingGraph(r, 7, 1, 9)
		return modelBenchmarkDataQuestion("有向容量图，cost表示容量，need忽略。源点0，汇点nodes-1。割由包含源且不含汇的顶点集合S唯一确定，容量为所有S到其补集的边容量之和（反向边不计）。返回 {flow:最大流值,cut_count:达到最小割容量的不同S数量}。没有源汇路径时最大流为0，仍计算全部最小割。", data), benchmarkFlowCuts(data)
	case "bipartite-matching":
		data := benchmarkMatchingData{Left: 5, Right: 5, Edges: [][2]int{}}
		for left := 0; left < data.Left; left++ {
			for right := 0; right < data.Right; right++ {
				if r.int(0, 2) == 0 {
					data.Edges = append(data.Edges, [2]int{left, right})
				}
			}
		}
		return modelBenchmarkDataQuestion("二分图的left/right为两侧节点数（分别从0编号），edges为[left节点,right节点]。匹配不可复用任一端点。返回 {size:最大匹配边数,count:不同最大匹配的数量}；匹配按边集合区分，不按选择顺序区分；空图有一个大小0的匹配。", data), benchmarkMatching(data)
	case "scc-condensation":
		data := benchmarkRandomCodingGraph(r, 8, 1, 1)
		return modelBenchmarkDataQuestion("求有向图强连通分量，cost/need均忽略。返回 {components:[[每分量节点升序],...],edges_needed:最少需新增多少条任意有向边才能使整图强连通}；components按各分量最小节点升序，已有强连通图需0条。不要求返回具体新增边。", data), benchmarkSCC(data)
	case "weighted-edit-distance":
		word := func() string {
			text := make([]byte, r.int(5, 7))
			for i := range text {
				text[i] = byte('a' + r.int(0, 3))
			}
			return string(text)
		}
		data := benchmarkEditData{Source: word(), Target: word(), Insert: r.int(1, 3), Delete: r.int(1, 3), Replace: r.int(1, 4), Transpose: r.int(1, 3)}
		return modelBenchmarkDataQuestion("求明确定义的 optimal-string-alignment 距离，不是任意重叠交换的 Damerau 距离。D[i,0]=i*delete，D[0,j]=j*insert；D[i,j]取D[i-1,j]+delete、D[i,j-1]+insert、D[i-1,j-1]+(source[i-1]==target[j-1]?0:replace)的最小值；若i,j>=2且source[i-1]==target[j-2]、source[i-2]==target[j-1]，还可取D[i-2,j-2]+transpose。返回D[len(source),len(target)]整数。", data), benchmarkEditDistance(data)
	case "lis-count":
		data := benchmarkCodingInts(r, 13, -4, 8)
		return modelBenchmarkDataQuestion("给定整数数组，求严格递增子序列（不要求连续）。返回 {length:最长长度,count:达到最长长度的子序列数量}。按选中的下标集合区分，相同值但下标不同分别计数，不能把相等值相接。", data), benchmarkLIS(data)
	case "range-update-query":
		data := benchmarkRangeData{Initial: benchmarkCodingInts(r, 10, -9, 12), Operations: []benchmarkRangeOperation{}}
		for i := 0; i < 22; i++ {
			left, right := r.int(0, 9), r.int(0, 9)
			if left > right {
				left, right = right, left
			}
			kind := "add"
			if i%3 == 2 || i == 21 {
				kind = "sum"
			}
			data.Operations = append(data.Operations, benchmarkRangeOperation{Op: kind, Left: left, Right: right, Delta: r.int(-6, 8)})
		}
		return modelBenchmarkDataQuestion("从initial开始按operations依次执行。下标从0开始、left/right两端均包含；op=add时将该区间每个元素加delta；op=sum时查询区间和，delta忽略。返回所有sum查询的结果，保持查询顺序。", data), benchmarkRangeQueries(data)
	case "bitmask-shortest-path":
		graph := benchmarkRandomCodingGraph(r, 7, 0, 6)
		for i := range graph.Edges {
			graph.Edges[i].Need = r.int(0, 7)
		}
		data := benchmarkKeyGraph{Nodes: graph.Nodes, Edges: graph.Edges, Keys: benchmarkCodingInts(r, graph.Nodes, 0, 7)}
		return modelBenchmarkDataQuestion("有向带权图，节点keys为3种永久钥匙的位掩码（1/2/4，可组合）。从节点0、空钥匙开始，进入节点立即获得该节点所有钥匙（含起点）。只有持有边need的全部钥匙才可走该边，先检查need再进入终点取钥匙；边cost非负，可为0，钥匙不消耗、允许重访。返回到nodes-1的最小总cost，无可行路径返回-1。", data), benchmarkKeyShortestPath(data)
	default:
		return generateModelBenchmarkD6(f, r)
	}
}

const benchmarkCodingInfinity = 1 << 28

func benchmarkNegativePaths(data benchmarkCodingGraph) []any {
	distance := make([]int, data.Nodes)
	for i := 1; i < data.Nodes; i++ {
		distance[i] = benchmarkCodingInfinity
	}
	for pass := 1; pass < data.Nodes; pass++ {
		for _, edge := range data.Edges {
			if distance[edge.From] < benchmarkCodingInfinity {
				distance[edge.To] = min(distance[edge.To], distance[edge.From]+edge.Cost)
			}
		}
	}
	negative := make([]bool, data.Nodes)
	for _, edge := range data.Edges {
		if distance[edge.From] < benchmarkCodingInfinity && distance[edge.To] > distance[edge.From]+edge.Cost {
			negative[edge.To] = true
		}
	}
	for pass := 0; pass < data.Nodes; pass++ {
		for _, edge := range data.Edges {
			negative[edge.To] = negative[edge.To] || negative[edge.From]
		}
	}
	out := make([]any, data.Nodes)
	for i := range out {
		switch {
		case negative[i]:
			out[i] = "-inf"
		case distance[i] == benchmarkCodingInfinity:
			out[i] = "unreachable"
		default:
			out[i] = distance[i]
		}
	}
	return out
}

func benchmarkFlowCuts(data benchmarkCodingGraph) map[string]any {
	n := data.Nodes
	residual := make([][]int, n)
	for i := range residual {
		residual[i] = make([]int, n)
	}
	for _, edge := range data.Edges {
		residual[edge.From][edge.To] += edge.Cost
	}
	flow := 0
	for {
		parent := make([]int, n)
		for i := range parent {
			parent[i] = -1
		}
		parent[0] = 0
		queue := []int{0}
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			for next, capacity := range residual[node] {
				if capacity > 0 && parent[next] == -1 {
					parent[next] = node
					queue = append(queue, next)
				}
			}
		}
		if parent[n-1] == -1 {
			break
		}
		amount := benchmarkCodingInfinity
		for next := n - 1; next != 0; next = parent[next] {
			amount = min(amount, residual[parent[next]][next])
		}
		for next := n - 1; next != 0; next = parent[next] {
			previous := parent[next]
			residual[previous][next] -= amount
			residual[next][previous] += amount
		}
		flow += amount
	}
	count := 0
	for mask := 1; mask < 1<<n; mask += 2 {
		if mask>>(n-1)&1 != 0 {
			continue
		}
		capacity := 0
		for _, edge := range data.Edges {
			if mask>>edge.From&1 != 0 && mask>>edge.To&1 == 0 {
				capacity += edge.Cost
			}
		}
		if capacity == flow {
			count++
		}
	}
	return map[string]any{"flow": flow, "cut_count": count}
}

type benchmarkMatchingData struct {
	Left  int      `json:"left"`
	Right int      `json:"right"`
	Edges [][2]int `json:"edges"`
}

func benchmarkMatching(data benchmarkMatchingData) map[string]any {
	ways := map[int]int{0: 1}
	for left := 0; left < data.Left; left++ {
		next := map[int]int{}
		for mask, count := range ways {
			next[mask] += count
			for _, edge := range data.Edges {
				if edge[0] == left && mask>>edge[1]&1 == 0 {
					next[mask|1<<edge[1]] += count
				}
			}
		}
		ways = next
	}
	best, count := -1, 0
	for mask, number := range ways {
		size := 0
		for value := mask; value != 0; value &= value - 1 {
			size++
		}
		if size > best {
			best, count = size, number
		} else if size == best {
			count += number
		}
	}
	return map[string]any{"size": best, "count": count}
}

func benchmarkSCC(data benchmarkCodingGraph) map[string]any {
	edges := [][2]int{}
	for _, edge := range data.Edges {
		edges = append(edges, [2]int{edge.From, edge.To})
	}
	reach := benchmarkReachability(data.Nodes, edges)
	components, componentID := [][]int{}, make([]int, data.Nodes)
	for i := range componentID {
		componentID[i] = -1
	}
	for i := 0; i < data.Nodes; i++ {
		if componentID[i] >= 0 {
			continue
		}
		group := []int{}
		for j := i; j < data.Nodes; j++ {
			if j == i || reach[i][j] && reach[j][i] {
				componentID[j] = len(components)
				group = append(group, j)
			}
		}
		components = append(components, group)
	}
	in, out := make([]bool, len(components)), make([]bool, len(components))
	for _, edge := range data.Edges {
		a, b := componentID[edge.From], componentID[edge.To]
		if a != b {
			out[a], in[b] = true, true
		}
	}
	sources, sinks := 0, 0
	for i := range in {
		if !in[i] {
			sources++
		}
		if !out[i] {
			sinks++
		}
	}
	needed := max(sources, sinks)
	if len(components) == 1 {
		needed = 0
	}
	return map[string]any{"components": components, "edges_needed": needed}
}

type benchmarkEditData struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Insert    int    `json:"insert"`
	Delete    int    `json:"delete"`
	Replace   int    `json:"replace"`
	Transpose int    `json:"transpose"`
}

func benchmarkEditDistance(data benchmarkEditData) int {
	n, m := len(data.Source), len(data.Target)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
		dp[i][0] = i * data.Delete
	}
	for j := 0; j <= m; j++ {
		dp[0][j] = j * data.Insert
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := data.Replace
			if data.Source[i-1] == data.Target[j-1] {
				cost = 0
			}
			dp[i][j] = min(dp[i-1][j]+data.Delete, dp[i][j-1]+data.Insert, dp[i-1][j-1]+cost)
			if i >= 2 && j >= 2 && data.Source[i-1] == data.Target[j-2] && data.Source[i-2] == data.Target[j-1] {
				dp[i][j] = min(dp[i][j], dp[i-2][j-2]+data.Transpose)
			}
		}
	}
	return dp[n][m]
}

func benchmarkLIS(values []int) map[string]any {
	lengths, counts := make([]int, len(values)), make([]int, len(values))
	best, total := 0, 0
	for i, value := range values {
		lengths[i], counts[i] = 1, 1
		for j := 0; j < i; j++ {
			if values[j] < value {
				candidate := lengths[j] + 1
				if candidate > lengths[i] {
					lengths[i], counts[i] = candidate, counts[j]
				} else if candidate == lengths[i] {
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

type benchmarkRangeOperation struct {
	Op    string `json:"op"`
	Left  int    `json:"left"`
	Right int    `json:"right"`
	Delta int    `json:"delta"`
}
type benchmarkRangeData struct {
	Initial    []int                     `json:"initial"`
	Operations []benchmarkRangeOperation `json:"operations"`
}

func benchmarkRangeQueries(data benchmarkRangeData) []int {
	n := len(data.Initial)
	first, second := make([]int, n+2), make([]int, n+2)
	update := func(tree []int, index, value int) {
		for ; index < len(tree); index += index & -index {
			tree[index] += value
		}
	}
	sum := func(tree []int, index int) int {
		value := 0
		for ; index > 0; index -= index & -index {
			value += tree[index]
		}
		return value
	}
	add := func(left, right, value int) {
		left++
		right++
		update(first, left, value)
		update(first, right+1, -value)
		update(second, left, value*(left-1))
		update(second, right+1, -value*right)
	}
	prefix := func(index int) int { return index*sum(first, index) - sum(second, index) }
	for i, value := range data.Initial {
		add(i, i, value)
	}
	answers := []int{}
	for _, op := range data.Operations {
		if op.Op == "add" {
			add(op.Left, op.Right, op.Delta)
		} else {
			answers = append(answers, prefix(op.Right+1)-prefix(op.Left))
		}
	}
	return answers
}

type benchmarkKeyGraph struct {
	Nodes int                   `json:"nodes"`
	Edges []benchmarkCodingEdge `json:"edges"`
	Keys  []int                 `json:"keys"`
}

func benchmarkKeyShortestPath(data benchmarkKeyGraph) int {
	distance, visited := make([]int, data.Nodes*8), make([]bool, data.Nodes*8)
	for i := range distance {
		distance[i] = benchmarkCodingInfinity
	}
	distance[data.Keys[0]] = 0
	for {
		state := -1
		for i, value := range distance {
			if !visited[i] && value < benchmarkCodingInfinity && (state < 0 || value < distance[state]) {
				state = i
			}
		}
		if state < 0 {
			return -1
		}
		visited[state] = true
		node, keys := state/8, state%8
		if node == data.Nodes-1 {
			return distance[state]
		}
		for _, edge := range data.Edges {
			if edge.From == node && keys&edge.Need == edge.Need {
				next := edge.To*8 + (keys | data.Keys[edge.To])
				distance[next] = min(distance[next], distance[state]+edge.Cost)
			}
		}
	}
}

// benchmarkSortedKeys gives a stable ordering for count distributions.
func benchmarkSortedKeys(values map[int]int) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}
