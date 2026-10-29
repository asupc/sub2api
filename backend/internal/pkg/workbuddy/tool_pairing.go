// tool_pairing.go 出站请求体的工具配对修复：拆条合并（merge）→ 结果归批（repack）
// → 孤儿清理（cleanup），顺序与 hub build_upstream_body 一致（先合并拆条让 repack
// 看到完整批次，最后才对称裁剪孤儿）。
//
// 移植来源：
//   - workbuddy2api-panel internal/upstream/tool_pairing.go 的 cleanupOrphanToolCalls
//     （2026-10-14，含空 function.name 否决分支）；
//   - workbuddy2api-hub wb_proxy.py 的 merge_adjacent_tool_calls /
//     repack_tool_result_blocks（2026-10-29 全面对照移植；repack 含 #420 修复——
//     插在 tool_calls 与结果之间/结果中间的消息移到整批结果之后，不再直接放弃重排）。
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

// 修复链入口：按 hub 顺序调用合并/归批/清理，返回是否发生任何改动。
func RepairToolPairing(messages []any) ([]any, bool) {
	changedAny := false
	if out, ch := MergeAdjacentToolCalls(messages); ch {
		messages, changedAny = out, true
	}
	if out, ch := RepackToolResultBlocks(messages); ch {
		messages, changedAny = out, true
	}
	if out, ch := CleanupOrphanToolCalls(messages); ch {
		messages, changedAny = out, true
	}
	return messages, changedAny
}

// emptyMessageContent 判定 assistant 消息的 content 是否「无内容」。
// None / "" / [] 三种空形态都算：客户端对空内容的输出形态不一致，
// 拆条批次必须对任一形态都合并（漏掉 [] 正是最初的 11148 案例）。
func emptyMessageContent(v any) bool {
	switch c := v.(type) {
	case nil:
		return true
	case string:
		return c == ""
	case []any:
		return len(c) == 0
	}
	return false
}

// mergeReasoningContent 把 src 的 reasoning_content 折进 dst（两者都有时换行连接）。
// DeepSeek 多轮思考要求 reasoning_content 回传，合并消息时不能丢。
func mergeReasoningContent(dst, src map[string]any) {
	rc, _ := src["reasoning_content"].(string)
	if rc == "" {
		return
	}
	if prev, _ := dst["reasoning_content"].(string); prev != "" {
		dst["reasoning_content"] = prev + "\n" + rc
	} else {
		dst["reasoning_content"] = rc
	}
}

// MergeAdjacentToolCalls 把相邻的 assistant tool_calls 消息合并回一条。
//
// 部分 OpenAI 兼容 Agent 客户端把一个并行批次重放成多条相邻 assistant 消息、
// 每条只带一个 tool_call。DeepSeek 系上游对这种形态返回 400 code 11148
// （tool_call_sequence_broken）并报废会话（重试永远重放同一份历史，换号无用）。
// 合并声明即可恢复上游接受的形态。条件严格、不发明语义：
//   - 必须相邻；
//   - 后条 content 必须为空（nil/""/[]）；
//   - 前条必须是带 tool_calls 的 assistant。
//
// 第二种形态：纯字符串 content 的 assistant 折进前面无 content 的 tool_calls
// assistant（同一批次的另一种拆分顺序）。两种形态都保留 reasoning_content
// （DeepSeek 多轮思考要求回传）。
func MergeAdjacentToolCalls(messages []any) ([]any, bool) {
	if len(messages) < 2 {
		return messages, false
	}
	out := make([]any, 0, len(messages))
	changed := false
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			out = append(out, m)
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" || len(out) == 0 {
			out = append(out, m)
			continue
		}
		prev, ok := out[len(out)-1].(map[string]any)
		if !ok {
			out = append(out, m)
			continue
		}
		if prole, _ := prev["role"].(string); prole != "assistant" {
			out = append(out, m)
			continue
		}
		prevCalls, prevHasCalls := prev["tool_calls"].([]any)
		tcs, hasCalls := msg["tool_calls"].([]any)
		// 形态 1：本条声明 tool_calls 且 content 为空 → 并进前条。
		if hasCalls && len(tcs) > 0 && prevHasCalls && len(prevCalls) > 0 && emptyMessageContent(msg["content"]) {
			prev["tool_calls"] = append(append([]any{}, prevCalls...), tcs...)
			mergeReasoningContent(prev, msg)
			changed = true
			continue
		}
		// 形态 2：本条无 tool_calls、纯字符串 content → 折进前面无 content 的 tool_calls 条。
		if _, has := msg["tool_calls"]; !has {
			if content, isStr := msg["content"].(string); isStr && content != "" &&
				prevHasCalls && len(prevCalls) > 0 && emptyMessageContent(prev["content"]) {
				prev["content"] = content
				mergeReasoningContent(prev, msg)
				changed = true
				continue
			}
		}
		out = append(out, m)
	}
	if !changed {
		return messages, false
	}
	return out, true
}

// RepackToolResultBlocks 保证 tool_calls 批次与其结果相邻。
//
// 上游要求 role:tool 结果紧跟发起调用的 assistant 消息、中间不能插任何消息。
// Codex 的 image_resize_notice（developer 消息）会插在 tool 输出之后、并行调用时
// 落在两条结果之间，配对读作断裂，上游拒整条请求（11148）并报废会话。本函数
// 只重排：结果不变、相对顺序不变，「插队」消息（含结果之前的第一条）统一移到
// 整批结果之后（#420 修复：此前遇到 tool_calls 与首条结果之间的插入消息会直接
// 放弃重排，配对依旧断裂）。
func RepackToolResultBlocks(messages []any) ([]any, bool) {
	if len(messages) < 3 {
		return messages, false
	}
	out := make([]any, 0, len(messages))
	changed := false
	i := 0
	for i < len(messages) {
		m := messages[i]
		msg, ok := m.(map[string]any)
		if !ok {
			out = append(out, m)
			i++
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			out = append(out, m)
			i++
			continue
		}
		calls, ok := msg["tool_calls"].([]any)
		if !ok || len(calls) == 0 {
			out = append(out, m)
			i++
			continue
		}
		want := map[string]bool{}
		for _, tci := range calls {
			if tc, ok := tci.(map[string]any); ok {
				if id, ok := tc["id"].(string); ok && id != "" {
					want[id] = true
				}
			}
		}
		out = append(out, m)
		i++
		// 收集紧跟的结果与插队消息：tool 且 id 属本批 → 结果；assistant → 批次结束；
		// 其余（user/developer/…）都是插队消息，移到整批结果之后。
		var results, between []any
	inner:
		for i < len(messages) {
			mm, ok := messages[i].(map[string]any)
			if !ok {
				break
			}
			role, _ := mm["role"].(string)
			switch {
			case role == "tool":
				id, _ := mm["tool_call_id"].(string)
				if !want[id] {
					// 不属于本批的结果：批次在此截断，交外层循环正常处理。
					break inner
				}
				results = append(results, mm)
				i++
			case role == "assistant":
				// 任何 assistant 消息都结束本批。
				break inner
			default:
				between = append(between, mm)
				i++
			}
		}
		if len(results) > 0 {
			out = append(out, results...)
			out = append(out, between...)
			if len(between) > 0 {
				changed = true
			}
		} else {
			out = append(out, between...)
		}
	}
	if !changed {
		return messages, false
	}
	return out, true
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
