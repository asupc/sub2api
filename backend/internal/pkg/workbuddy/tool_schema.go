// tool_schema.go 出站请求体的工具声明规整与 stop 序列归一化。
//
// 移植自 workbuddy2api-hub wb_proxy.py（2026-10-29 全面对照移植），并按
// workbuddy2api-panel 615dc75 / d6301fe / abed839 / 733942a 的实测边界扩充：
//   - normalize_tools / _normalize_tool_parameters / _deep_normalize_schema
//     （#451）：上游 Go schema 校验器（400 code 11129 "invalid function call
//     parameters"）要求 function.parameters 形态严格——type:"object"、
//     properties 为字典、required 为无 null 的字符串数组、类型名为标准 JSON
//     Schema 类型；无参工具发 "parameters": {}（缺顶层 type）也触发 11129。
//   - normalize_stop（v1.7.0）：上游 Go struct 把 stop 定死为 []string，客户端
//     按 OpenAI 规范发单字符串直接 400 code 11101（cannot unmarshal string into
//     Go struct field Request.stop of type []string）。
package workbuddy

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// toolNameRe 上游 Go 校验器只接受 ^[A-Za-z0-9_-]{1,64}$ 形态的 function.name
// （hub #726/b04868e 实测：tools[N].function.name 不合法即整单 400 code 11133）。
var toolNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

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

// schemaDataKeys 取值是**数据**而不是 schema 的关键字：default / const / enum /
// example(s) 里的对象就是客户端声明的取值本身，里面出现 type、items、required、
// null 也只是数据。规整只作用于 schema，绝不递归进这些值。
var schemaDataKeys = map[string]bool{
	"default":  true,
	"const":    true,
	"enum":     true,
	"example":  true,
	"examples": true,
}

// schemaContainerKeys JSON Schema 的容器级 map：键是属性名/定义名，绝不能当
// Schema 关键字判定（属性名叫 items 时，dependentRequired 的 ["items"] 会被误改写）。
var schemaContainerKeys = map[string]bool{
	"properties":        true,
	"patternProperties": true,
	"$defs":             true,
	"definitions":       true,
	"dependentSchemas":  true,
	"dependentRequired": true,
	"dependencies":      true,
}

// schemaNumberKeys / schemaBoolKeys / schemaStringKeys 三组关键字的取值类型同样被
// 上游按 Go 强类型反序列化校验，类型不符即整体拒收（11129）：
//
//	minimum:"1"        → 400 11129      minimum:1        → 200
//	uniqueItems:"true" → 400 11129      uniqueItems:true → 200
//	description:123    → 400 11129      description:"x"  → 200
//	enum:"x"           → 400 11129      enum:["x"]       → 200
//	additionalProperties:"false" → 400 11129
var schemaNumberKeys = []string{
	"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
	"minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties",
}
var schemaBoolKeys = []string{"uniqueItems", "deprecated", "readOnly", "writeOnly"}
var schemaStringKeys = []string{"description", "title", "format", "pattern"}

// deepNormalizeSchema 递归清洗 JSON Schema 以通过上游 Go 校验器（11129）。
//
// 只修复非法形态，不删合规字段、不改语义参数（hub #423：schema 合法性归一是
// 上游传输层要求，与 WB_PRESERVE_REQUEST_CONTRACT 类应用层改写无关）：
//  0. 关键字取值类型纠偏（见 coerceSchemaKeywords）；
//  1. 剔除值为 nil 的关键字（数据关键字 default/const/enum/example(s) 除外）；
//  2. 类型名归一（str/int/dict 等别名 → 标准类型；非法类型按形态推断或删除）；
//  3. required 清洗（去 nil/空串/非字符串，重复项去重）；
//  4. items 解包 tuple 形态；
//  5. pattern 的 `\_` → `_`、`\uXXXX` → `\x{XXXX}`（RE2 不认 \u），译后仍编不过
//     就丢掉该 pattern（丢一条正则 < 整颗工具被上游否掉）；
//  6. 只有 enum 没有 type 时按取值推断类型；
//  7. 递归子树：容器键的键是用户属性名，只遍历其子值；数据关键字整段跳过——
//     否则改写的是模型看到的默认值/枚举项。
func deepNormalizeSchema(s map[string]any) map[string]any {
	if s == nil {
		return s
	}
	coerceSchemaKeywords(s)
	// 1. nil 关键字剔除（数据关键字保留：const:null/default:null 是有意义的取值）。
	for k, v := range s {
		if v == nil && !schemaDataKeys[k] {
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
	// 4. required 清洗 + 去重（["a","a"] 实测同样 400 11129）。
	if req, ok := s["required"]; ok {
		switch r := req.(type) {
		case []any:
			cleaned := make([]any, 0, len(r))
			seen := make(map[string]bool, len(r))
			for _, x := range r {
				if x == nil {
					continue
				}
				if xs, ok := x.(string); ok {
					xs = strings.TrimSpace(xs)
					if xs == "" || seen[xs] {
						continue
					}
					seen[xs] = true
					cleaned = append(cleaned, xs)
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
	// 5. items 归一（tuple 形态取首个字典；无字典回落 string）。
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
	// 6. pattern 转义修复（`\_` 与 JS 的 `\uXXXX`）。
	if p, ok := s["pattern"].(string); ok {
		if fixed, compiled := re2Pattern(p); compiled {
			s["pattern"] = fixed
		} else {
			delete(s, "pattern")
		}
	}
	if props, ok := s["patternProperties"].(map[string]any); ok {
		rebuilt := false
		fixed := make(map[string]any, len(props))
		for k, v := range props {
			if strings.Contains(k, `\_`) {
				k = strings.ReplaceAll(k, `\_`, `_`)
				rebuilt = true
			}
			fixed[k] = v
		}
		if rebuilt {
			s["patternProperties"] = fixed
		}
	}
	// 7. enum 缺 type 时按取值推断。
	if _, hasType := s["type"]; !hasType {
		if e, ok := s["enum"].([]any); ok && len(e) > 0 {
			if t := inferEnumType(e); t != "" {
				s["type"] = t
			}
		}
	}
	// 8. 递归子树（容器键只遍历子值，数据关键字整段跳过）。
	for k, v := range s {
		if schemaDataKeys[k] {
			continue
		}
		if schemaContainerKeys[k] {
			if c, ok := v.(map[string]any); ok {
				for childKey, child := range c {
					if child == nil {
						delete(c, childKey)
						continue
					}
					deepNormalizeSchemaValue(child)
				}
			}
			continue
		}
		deepNormalizeSchemaValue(v)
	}
	return s
}

// deepNormalizeSchemaValue 递归进入任意子值（对象 → Schema，数组 → 逐项）。
func deepNormalizeSchemaValue(node any) {
	switch n := node.(type) {
	case map[string]any:
		deepNormalizeSchema(n)
	case []any:
		for _, child := range n {
			deepNormalizeSchemaValue(child)
		}
	}
}

// coerceSchemaKeywords 把三组关键字的取值纠成上游能接受的基本类型；纠不动就删键
// （留着一个非法值等于保证整颗工具必然 400，删掉只是丢掉一条约束或注释）。
func coerceSchemaKeywords(n map[string]any) {
	for _, k := range schemaNumberKeys {
		if v, ok := n[k]; ok {
			if num, ok := schemaNumber(v); ok {
				n[k] = num
			} else {
				delete(n, k)
			}
		}
	}
	for _, k := range schemaBoolKeys {
		if v, ok := n[k]; ok {
			if b, ok := schemaBool(v); ok {
				n[k] = b
			} else {
				delete(n, k)
			}
		}
	}
	for _, k := range schemaStringKeys {
		if v, ok := n[k]; ok {
			if _, isStr := v.(string); !isStr {
				delete(n, k)
			}
		}
	}
	if v, ok := n["additionalProperties"]; ok {
		if _, isSchema := v.(map[string]any); !isSchema {
			if b, ok := schemaBool(v); ok {
				n["additionalProperties"] = b
			} else {
				delete(n, "additionalProperties")
			}
		}
	}
	for _, k := range []string{"enum", "examples"} {
		if v, ok := n[k]; ok {
			if _, isList := v.([]any); !isList {
				delete(n, k)
			}
		}
	}
}

// schemaNumber 纠数值关键字：数字原样，纯数字字符串转数值，其余判非法。
func schemaNumber(v any) (any, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, false
		}
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		return f, true
	}
	return nil, false
}

// schemaBool 纠布尔关键字：bool 原样，"true"/"false" 转义，其余判非法。
func schemaBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// reuEscape 匹配 pattern 里的 JS `\uXXXX` / `\u{XXXX}` 转义。
var reuEscape = regexp.MustCompile(`\\u\{?[0-9A-Fa-f]{4,6}\}?`)

// re2Pattern 把 `\_` 与 JS 的 `\uXXXX` 译成 RE2 能编译的形态；不含 `\u` 时零开销
// （不做编译验证）。译完仍编不过（混了别的 RE2 不认的语法）返回 false，调用方丢弃
// 该 pattern——丢它只是放宽一条本地正则，留着等于让整包工具被上游否掉（issue #276）。
func re2Pattern(p string) (string, bool) {
	if strings.Contains(p, `\_`) {
		p = strings.ReplaceAll(p, `\_`, `_`)
	}
	if !strings.Contains(p, `\u`) {
		return p, true
	}
	q := reuEscape.ReplaceAllStringFunc(p, func(m string) string {
		return `\x{` + strings.Trim(m[2:], "{}") + `}`
	})
	if _, err := regexp.Compile(q); err != nil {
		return "", false
	}
	return q, true
}

// inferEnumType 从 enum 取值推断 JSON Schema 类型；推不出唯一类型时返回空（不改写）。
func inferEnumType(values []any) string {
	var strs, nums, ints, bools int
	for _, v := range values {
		switch x := v.(type) {
		case string:
			strs++
		case bool:
			bools++
		case float64:
			nums++
			if x == math.Trunc(x) {
				ints++
			}
		default:
			return "" // null / 对象 / 数组：类型不唯一，不猜
		}
	}
	switch {
	case strs > 0 && nums == 0 && bools == 0:
		return "string"
	case nums > 0 && strs == 0 && bools == 0:
		if ints == nums {
			return "integer"
		}
		return "number"
	case bools > 0 && strs == 0 && nums == 0:
		return "boolean"
	}
	return ""
}

// inlineTopLevelRef 处理 parameters 顶层 `$ref`（panel 733942a）：整份参数 schema
// 指向 $defs 时上游一刀拒收（400 11129），而嵌套 $ref 实测 200——只有顶层要处理。
// 能就地展开就展开（保住真实参数定义），展不开（非 #/$defs/、目标缺失、循环）也必须
// 删掉：留着等于保证这颗工具每次 400，删掉至少退化成无参工具。
func inlineTopLevelRef(params map[string]any) {
	ref, ok := params["$ref"].(string)
	if !ok {
		return
	}
	delete(params, "$ref")
	defs, hasDefs := params["$defs"].(map[string]any)
	if !hasDefs || !strings.HasPrefix(ref, "#/$defs/") {
		return
	}
	target := resolveSchemaRef(strings.TrimPrefix(ref, "#/$defs/"), defs, 0)
	if target == nil {
		return
	}
	for k, v := range target {
		if _, dup := params[k]; !dup {
			params[k] = v
		}
	}
}

// resolveSchemaRef 按 $defs 名取目标定义并解开其自身 $ref 链（深度上限防环）。
func resolveSchemaRef(name string, defs map[string]any, depth int) map[string]any {
	if depth > 8 {
		return nil
	}
	t, ok := defs[name].(map[string]any)
	if !ok {
		return nil
	}
	if next, ok := t["$ref"].(string); ok && strings.HasPrefix(next, "#/$defs/") {
		clone := make(map[string]any, len(t))
		for k, v := range t {
			clone[k] = v
		}
		delete(clone, "$ref")
		if resolved := resolveSchemaRef(strings.TrimPrefix(next, "#/$defs/"), defs, depth+1); resolved != nil {
			for k, v := range resolved {
				if _, dup := clone[k]; !dup {
					clone[k] = v
				}
			}
		}
		return clone
	}
	return t
}

// normalizeToolParameters 单个 function 声明的 parameters 规整（11129）。
// parameters 缺失/非对象 → {"type":"object","properties":{}}（无参工具发 "{}"
// 同样被上游拒）；对象 → 顶层 $ref 展开 + 强制 type:"object" + properties 字典 +
// 递归清洗。
func normalizeToolParameters(fn map[string]any) {
	params, ok := fn["parameters"].(map[string]any)
	if !ok || params == nil {
		fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
		return
	}
	inlineTopLevelRef(params)
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
	norm = dropUnusableTools(norm)
	if len(norm) > 0 {
		obj["tools"] = norm
	} else {
		delete(obj, "tools")
	}
}

// dropUnusableTools 归一化后的名字合法性过滤（hub normalize_tools 尾段，#726）：
//
// 上游 Go 校验器只接受 ^[A-Za-z0-9_-]{1,64}$ 的 function.name，其余整单 400
// code 11133（param: tools[N].function.name）。两类条目过不了这道门：
//   - 服务端工具声明（如 {"type":"web_search"}）：normalizeTools 把任何非
//     function 的 type 强改成 "function"，但条目没有 function 对象——空手
//     抵达上游即 400；
//   - 名字含方括号外字符（点号/斜杠/CJK/空格）、空名或缺失。
//
// 只丢弃、不改名：改名会让模型吐出的 tool_calls 名与客户端声明对不上，把一次
// 400 换成一个静默失效的工具。同名只留第一条（与 namespace 展开的去重策略
// 一致）。合法工具逐字不变。
func dropUnusableTools(norm []any) []any {
	valid := make([]any, 0, len(norm))
	seen := map[string]bool{}
	for _, t := range norm {
		item, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := item["function"].(map[string]any)
		if !ok {
			continue // 无 function 对象（纯服务端声明改写产物），丢弃。
		}
		name, _ := fn["name"].(string)
		name = strings.TrimSpace(name)
		if !toolNameRe.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		valid = append(valid, item)
	}
	return valid
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
