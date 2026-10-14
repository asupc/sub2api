// tool_pairing.go 出站请求体的孤儿 tool_call↔tool 配对清理。
//
// 移植自参考仓库 workbuddy2api-panel internal/upstream/tool_pairing.go 的
// cleanupOrphanToolCalls（2026-10-14，含空 function.name 否决分支），适配本包
// 的 PrepareChatBody 出站归一化管道。mergeAdjacentToolCalls /
// repackToolResultBlocks 未移植——本函数独立成立，但缺其保护（见调用处注释）。
//
// 背景：OpenAI 兼容协议要求带 tool_calls 的 assistant 消息，其每一个 tool_call id
// 都必须有对应的一条 role:tool 结果消息；反之 role:tool 消息也必须有对应的前置
// tool_call。缺任一侧，WorkBuddy 上游都会以 HTTP 400 拒绝整个请求。
//
// 工具执行失败时（参数非法、超时、工具不存在……）客户端会把 assistant 的 tool_calls
// 持久化进会话历史，却写不回结果消息。这条坏历史随后被每次请求原样重放——上游对之后
// 每一条用户消息都返回 400，整条会话报废。网关是最后一道防线：发出请求前剔除无法配对
// 的条目让会话自愈，宁可丢一轮工具上下文，也好过整条会话死亡。
//
// 另一条致命形态（线上 11133）：上游流式响应在工具调用 name 分片到达前异常结束时，
// 聚合器按占位符产出 name:"" 的条目，客户端把它写进会话历史；下一轮原样转发，
// 上游判 400 {"code":11133,"extError":{"code":"model_param_invalid"}}
// （tool_calls[i].function.name must be a non-empty string），且**带不带配对结果
// 都会拒**——整条会话从此每轮 503，只能手动清空历史。
package workbuddy

import "strings"

// CleanupOrphanToolCalls 剔除无法配对的 tool_call 与 tool 结果，并否决 function.name
// 为空（含纯空白）的脏调用。语义与上游 cleanupOrphanToolCalls 一致：
//
//   - 收集全线 role:tool 消息的 tool_call_id（结果集）与 assistant.tool_calls[].id（调用集）；
//   - 一批 assistant.tool_calls 按 keepCalls 对称裁剪：只留有结果配对且 name 非空的调用
//     （部分保留不会留下无结果的 tool_call），过滤后为空才删掉整个 tool_calls 键；
//   - role:tool 只在对应 tool_call 被保留时才保留，否则删除整条消息；
//   - 无任何工具流量 → 原 slice 原样返回，changed=false（零分配零改动）。
//
// 这是「让请求通过」的安全网：只要存在合法配对就整段保留这些字段，绝不吞掉正确配对。
// 返回清理后的 slice（无改动时等于原 slice，勿依赖其是否新分配）及是否发生删除。
//
// 注意：只按 id 对称删除、不重排——上游对「声明 tool_calls 的 assistant 之后必须紧跟
// 自己的结果」有 11148 硬校验，而拆条/插队的报文整形（merge/repack）在本包暂无实现。
func CleanupOrphanToolCalls(messages []any) ([]any, bool) {
	if len(messages) == 0 {
		return messages, false
	}
	callIDs := map[string]bool{}
	resultIDs := map[string]bool{}
	nameBadIDs := map[string]bool{}
	hasTraffic := false
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		switch msg["role"] {
		case "tool":
			if id, ok := msg["tool_call_id"].(string); ok && id != "" {
				resultIDs[id] = true
				hasTraffic = true
			}
		case "assistant":
			if tcs, ok := msg["tool_calls"].([]any); ok {
				for _, tci := range tcs {
					tc, ok := tci.(map[string]any)
					if !ok {
						continue
					}
					if id, ok := tc["id"].(string); ok && id != "" {
						callIDs[id] = true
						if toolCallNameEmpty(tc) {
							nameBadIDs[id] = true
						}
						hasTraffic = true
					}
				}
			}
		}
	}
	if !hasTraffic {
		return messages, false
	}
	// keepCalls：调用 id 是否双侧齐全（调用存在且结果存在）。重复 id 与乱序均按集合处理。
	//
	// 追加否决条件：function.name 为空（含纯空白）的调用一律不保留。上游对这类
	// 条目直接判 11133 model_param_invalid，且带不带配对结果都会拒——带结果的脏条目
	// 原本会命中 keepCalls 原样出站，正是线上 503 的成因。删除动作完全复用两侧对称裁剪。
	keepCalls := map[string]bool{}
	for id := range callIDs {
		if nameBadIDs[id] {
			continue
		}
		if resultIDs[id] {
			keepCalls[id] = true
		}
	}
	changed := false
	// 1) assistant.tool_calls：按 keepCalls 对称裁剪——只留有结果的调用，过滤后为空则删键。
	//
	// 「批内每个 id 都齐才整批保留，否则删掉整个 tool_calls 键」会留下无主结果：
	// 批 [c1,c2] 只回了 c1 时，调用侧整批被删，而 tool{c1} 仍按 id 命中 keepCalls
	// 得以保留 —— 出站载荷于是变成「无 tool_calls 的 assistant + 孤儿 tool」，
	// 上游判 11148 并顶死整条会话。两侧共用同一份 keepCalls 按 id 对称裁剪，
	// 任何输入都不会再产生半截配对。
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		tcs, ok := msg["tool_calls"].([]any)
		if !ok || len(tcs) == 0 {
			continue
		}
		keptCalls := make([]any, 0, len(tcs))
		for _, tci := range tcs {
			tc, ok := tci.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := tc["id"].(string); keepCalls[id] {
				keptCalls = append(keptCalls, tc)
			}
		}
		if len(keptCalls) == len(tcs) {
			continue // 整批齐全：零改动
		}
		changed = true
		if len(keptCalls) == 0 {
			delete(msg, "tool_calls")
			continue
		}
		msg["tool_calls"] = keptCalls
	}
	// 2) role:tool 结果：只有对应 tool_call 被保留才保留；孤儿结果整条删除。
	kept := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			kept = append(kept, m)
			continue
		}
		if role, _ := msg["role"].(string); role == "tool" {
			id, _ := msg["tool_call_id"].(string)
			if !keepCalls[id] {
				changed = true
				continue
			}
		}
		kept = append(kept, m)
	}
	if !changed {
		return messages, false
	}
	return kept, true
}

// toolCallNameEmpty 判断 assistant.tool_calls[] 单项的 function.name 是否为空。
// 仅当 function 对象存在、name 字段存在且为空白字符串时判真；
// function / name 字段缺失时判假——历史报文里存在不带 function 的占位条目，
// 上游不会因此拒参，这里不扩大打击面。
func toolCallNameEmpty(tc map[string]any) bool {
	fn, ok := tc["function"].(map[string]any)
	if !ok {
		return false
	}
	name, ok := fn["name"].(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(name) == ""
}
