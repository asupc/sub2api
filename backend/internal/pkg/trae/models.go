package trae

// ModelNames 网关对外暴露的静态兜底模型清单（读不到客户端 state.vscdb 下发清单时用；
// 服务器场景默认就用这份静态清单，可通过 WithModels 注入覆盖）。
//
// 内容口径（源 payload.rs，2026-09-30 逐条实测修正）：
//  1. 客户端 solo_work_lite 分组的内容（15 条，保序），减去一条实测被上游拒绝的
//     第三方路由条目 openrouter//stealth/ox-alpha（4001 param is invalid）；
//  2. 历史对外名 6 条：不在客户端清单里，但逐个实测上游仍然接受，删掉会断老调用方；
//     deepseek-v4-flash 同时是历史默认模型；
//  3. 已剔除 doubao-seed-2.0-code（属于 solo_coder 分组，在 solo_work_lite 下实测 4001）。
//
// ⚠️ 改这里的名字要同时看 ModelConfig——只有「与客户端清单不一致」的名字才需要在
// 那张表里登记；其余名字按派生规则发给上游（config_name = 名字、model_name = 名字__dev）。
var ModelNames = []string{
	// ---- 客户端 TraeWork 的 solo_work_lite 分组（顺序与客户端清单一致）----
	"Doubao-Seed-Evolving",
	"Doubao-Seed-2.1-Pro",
	"Doubao-Seed-2.1-Turbo",
	"step-5-preview",
	"glm-5.3",
	"glm-5.2",
	"deepseek-v4.1-flash",
	"DeepSeek-V4-Flash-Official",
	"DeepSeek-V4-Pro-Official",
	"kimi-k3",
	"kimi-k2.7-code",
	"kimi-k2.6",
	"minimax-m3",
	"qwen3.8-max",
	"qwen-3.7-plus",
	// ---- 历史对外名：不在客户端清单里，但实测上游仍然接受 ----
	"deepseek-v4-flash",
	"deepseek-v4-pro",
	"glm-5-turbo",
	"glm-5",
	"sagitta",
	"aquila",
}

// DefaultContextLength 静态兜底清单的上下文窗口（客户端清单里取不到时沿用）。
const DefaultContextLength int64 = 131072

// modelsCreatedAt /v1/models 条目的固定 created 时间戳（源 payload.rs 逐字：1_753_600_000）。
const modelsCreatedAt int64 = 1753600000

// ModelEntry /v1/models 的一条模型记录。
type ModelEntry struct {
	Name string
	// ContextLength 声明口径（issue #53 三版）：官方目录 max > dev ⇒ 声明
	// max（支持 Max Mode，请求时会注入 is_max_mode）；否则 dev（__dev 通道
	// 实际窗口）；目录缺失 ⇒ 默认兜底，不猜数字。
	ContextLength int64 // 0 → DefaultContextLength
	// MaxMode 说明字段：true = 该模型支持 Max Mode（context_length 已按其
	// 上限声明）。OpenAI 兼容客户端会忽略未知字段。
	MaxMode bool
}

// ModelsResponseFor 组装 OpenAI /v1/models 形状的响应体。
//
// 静态清单 + 可注入覆盖：entries 为空时回落 ModelNames（静态兜底），
// 保证「列模型」这一开箱能力不因清单缺失而失效。
func ModelsResponseFor(entries []ModelEntry) map[string]any {
	if len(entries) == 0 {
		entries = make([]ModelEntry, 0, len(ModelNames))
		for _, name := range ModelNames {
			entries = append(entries, ModelEntry{Name: name})
		}
	}
	data := make([]any, 0, len(entries))
	for _, entry := range entries {
		context := entry.ContextLength
		if context <= 0 {
			context = DefaultContextLength
		}
		item := map[string]any{
			"id":             entry.Name,
			"object":         "model",
			"created":        modelsCreatedAt,
			"owned_by":       "trae",
			"context_length": context,
		}
		if entry.MaxMode {
			item["max_mode"] = true
		}
		data = append(data, item)
	}
	return map[string]any{
		"object": "list",
		"data":   data,
	}
}

// ModelsResponse 静态兜底清单的 /v1/models 响应。
func ModelsResponse() map[string]any {
	return ModelsResponseFor(nil)
}
