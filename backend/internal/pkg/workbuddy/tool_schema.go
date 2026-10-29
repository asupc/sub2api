// tool_schema.go 出站请求体的工具声明规整与 stop 序列归一化。
//
// 移植自 workbuddy2api-hub wb_proxy.py（2026-10-29 全面对照移植）：
//   - normalize_tools / _normalize_tool_parameters / _deep_normalize_schema
//     （#451）：上游 Go schema 校验器（400 code 11129 "invalid function call
//     parameters"）要求 function.parameters 形态严格——type:"object"、
//     properties 为字典、required 为无 null 的字符串数组、类型名为标准 JSON
//     Schema 类型；无参工具发 "parameters": {}（缺顶层 type）也触发 11129。
//   - normalize_stop（v1.7.0）：上游 Go struct 把 stop 定死为 []string，客户端
//     按 OpenAI 规范发单字符串直接 400 code 11101（cannot unmarshal string into
//     Go struct field Request.stop of type []string）。
package workbuddy

import "strings"

// schemaTypeMap / schemaValidTypes 类型名归一表（hub _SCHEMA_TYPE_MAP 同款）。
var schemaTypeMap = map[string]string{
	"str":   "string",
	"text":  "string",
	"int":   "integer",
	"bool":  "boolean",
	"dict":  "object",
	"list":  "array",
	"float": "number",
}

var schemaValidTypes = map[string]bool{
	"string": true, "number": true, "integer": true, "boolean": true,
	"array": true, "object": true, "null": true,
}

// deepNormalizeSchema 递归清洗 JSON Schema 以通过上游 Go 校验器（11129）。
// 只修复非法形态，不删合规字段、不改语义参数（hub #423：schema 合法性归一是
// 上游传输层要求，与 WB_PRESERVE_REQUEST_CONTRACT 类应用层改写无关）：
//  1. description/title/format/pattern/items/default 为 nil 的元数据字段剔除
//     （上游严格拒绝 null）；
//  2. 类型名归一（str/int/dict 等别名 → 标准类型；非法类型按形态推断或删除；
//     数组形态逐项归一）；
//  3. object 类型恒补 properties 字典；
//  4. required 清洗（去 nil/空串，单字符串包数组，全空则删）；
//  5. items：数组形态取首个字典并递归（无字典回落 {"type":"string"}），nil 删；
//  6. properties / $defs / definitions / anyOf / oneOf / allOf 递归。
func deepNormalizeSchema(s map[string]any) map[string]any {
	if s == nil {
		return s
	}
	// 1. null 元数据字段剔除。
	for _, k := range []string{"description", "title", "format", "pattern", "items", "default"} {
		if v, ok := s[k]; ok && v == nil {
			delete(s, k)
		}
	}
	// 2. 类型名归一。
	if typ, ok := s["type"]; ok {
		switch t := typ.(type) {
		case string:
			lower := strings.ToLower(strings.TrimSpace(t))
			if mapped, ok := schemaTypeMap[lower]; ok {
				s["type"] = mapped
			} else if !schemaValidTypes[lower] {
				if _, has := s["properties"]; has {
					s["type"] = "object"
				} else if _, has := s["items"]; has {
					s["type"] = "array"
				} else {
					delete(s, "type")
				}
			} else {
				s["type"] = lower
			}
		case []any:
			out := make([]any, 0, len(t))
			for _, e := range t {
				es, ok := e.(string)
				if !ok || strings.TrimSpace(es) == "" {
					continue
				}
				lower := strings.ToLower(es)
				if mapped, ok := schemaTypeMap[lower]; ok {
					lower = mapped
				}
				out = append(out, lower)
			}
			s["type"] = out
		}
	}
	// 3. object 恒补 properties 字典。
	if s["type"] == "object" || (func() bool { _, has := s["properties"]; return has && s["type"] == nil })() {
		if s["type"] == nil {
			s["type"] = "object"
		}
		if props, ok := s["properties"].(map[string]any); !ok || props == nil {
			s["properties"] = map[string]any{}
		}
	}
	// 4. required 清洗。
	if req, ok := s["required"]; ok {
		switch r := req.(type) {
		case []any:
			cleaned := make([]any, 0, len(r))
			for _, x := range r {
				if x == nil {
					continue
				}
				if xs, ok := x.(string); ok && strings.TrimSpace(xs) == "" {
					continue
				}
				cleaned = append(cleaned, x)
			}
			if len(cleaned) > 0 {
				s["required"] = cleaned
			} else {
				delete(s, "required")
			}
		case string:
			if strings.TrimSpace(r) != "" {
				s["required"] = []any{strings.TrimSpace(r)}
			} else {
				delete(s, "required")
			}
		default:
			delete(s, "required")
		}
	}
	// 5. items 归一。
	if items, ok := s["items"]; ok {
		switch it := items.(type) {
		case []any:
			var first map[string]any
			for _, e := range it {
				if m, ok := e.(map[string]any); ok {
					first = m
					break
				}
			}
			if first != nil {
				s["items"] = deepNormalizeSchema(first)
			} else {
				s["items"] = map[string]any{"type": "string"}
			}
		case map[string]any:
			s["items"] = deepNormalizeSchema(it)
		case nil:
			delete(s, "items")
		}
	}
	// 6. properties 递归。
	if props, ok := s["properties"].(map[string]any); ok {
		for name, pv := range props {
			if pm, ok := pv.(map[string]any); ok {
				props[name] = deepNormalizeSchema(pm)
			}
		}
	}
	// 7. $defs / definitions 递归。
	for _, defKey := range []string{"$defs", "definitions"} {
		if defs, ok := s[defKey].(map[string]any); ok {
			for name, dv := range defs {
				if dm, ok := dv.(map[string]any); ok {
					defs[name] = deepNormalizeSchema(dm)
				}
			}
		}
	}
	// 8. 组合关键字递归。
	for _, combKey := range []string{"anyOf", "oneOf", "allOf"} {
		if comb, ok := s[combKey].([]any); ok {
			for _, sub := range comb {
				if sm, ok := sub.(map[string]any); ok {
					deepNormalizeSchema(sm)
				}
			}
		}
	}
	return s
}

// normalizeToolParameters 单个 function 声明的 parameters 规整（11129）。
// parameters 缺失/非对象 → {"type":"object","properties":{}}（无参工具发 "{}"
// 同样被上游拒）；对象 → 强制 type:"object" + properties 字典 + 递归清洗。
func normalizeToolParameters(fn map[string]any) {
	params, ok := fn["parameters"].(map[string]any)
	if !ok || params == nil {
		fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		return
	}
	if params["type"] != "object" {
		params["type"] = "object"
	}
	if props, ok := params["properties"].(map[string]any); !ok || props == nil {
		params["properties"] = map[string]any{}
	}
	deepNormalizeSchema(params)
}

// normalizeTools 规整 obj 的 tools 声明（hub normalize_tools）：
//   - 旧客户端的顶层 functions（无 tools 时）转换为 tools 形态；
//   - 顶层 name 风格的工具定义（无 function 包装）包装成 Chat Completions 形态
//     （strict 随迁）；
//   - 每项强制 type:"function" 并规整 function.parameters（11129）；
//   - 全部非法/空 → 删 tools 键。
func normalizeTools(obj map[string]any) {
	if funcs, ok := obj["functions"].([]any); ok {
		if _, hasTools := obj["tools"]; !hasTools {
			delete(obj, "functions")
			tools := make([]any, 0, len(funcs))
			for _, f := range funcs {
				if fn, ok := f.(map[string]any); ok {
					tools = append(tools, map[string]any{"type": "function", "function": fn})
				}
			}
			if len(tools) > 0 {
				obj["tools"] = tools
			}
		}
	}
	tools, ok := obj["tools"].([]any)
	if !ok || len(tools) == 0 {
		delete(obj, "tools")
		return
	}
	norm := make([]any, 0, len(tools))
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if _, hasFn := tool["function"]; !hasFn {
			// 顶层 name 风格 → 包装成 function 形态。
			if name, ok := tool["name"]; ok {
				fn := map[string]any{
					"name":        name,
					"description": tool["description"],
					"parameters":  tool["parameters"],
				}
				if fn["description"] == nil {
					fn["description"] = ""
				}
				if fn["parameters"] == nil {
					fn["parameters"] = map[string]any{}
				}
				if strict, ok := tool["strict"]; ok {
					fn["strict"] = strict
				}
				normalizeToolParameters(fn)
				norm = append(norm, map[string]any{"type": "function", "function": fn})
				continue
			}
		}
		item := map[string]any{}
		for k, v := range tool {
			item[k] = v
		}
		item["type"] = "function"
		if fn, ok := item["function"].(map[string]any); ok {
			fnCopy := map[string]any{}
			for k, v := range fn {
				fnCopy[k] = v
			}
			normalizeToolParameters(fnCopy)
			item["function"] = fnCopy
		}
		norm = append(norm, item)
	}
	if len(norm) > 0 {
		obj["tools"] = norm
	} else {
		delete(obj, "tools")
	}
}

// normalizeStop 归一化 stop 停止序列（hub normalize_stop，v1.7.0）。
// OpenAI 把 stop 声明成 string | array，客户端传单字符串完全合法；本上游的 Go
// 端却把该字段定死为 []string，收到字符串直接 400 code 11101。归一化成数组后
// 语义完全等价（OpenAI 里单字符串就是单元素数组）；空字符串 / 空数组 / null /
// 非法类型一律删字段（其含义都是「没有停止序列」，空值在上游同样判参数不合法）。
func normalizeStop(obj map[string]any) {
	val, present := obj["stop"]
	if !present {
		return
	}
	var out []any
	switch v := val.(type) {
	case string:
		if v != "" {
			out = []any{v}
		}
	case []any:
		for _, s := range v {
			if ss, ok := s.(string); ok && ss != "" {
				out = append(out, ss)
			}
		}
	}
	if len(out) > 0 {
		obj["stop"] = out
	} else {
		delete(obj, "stop")
	}
}
