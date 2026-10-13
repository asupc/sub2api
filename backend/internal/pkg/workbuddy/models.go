// models.go 上游模型目录探测（CN 双路并发 + global 多路 UA）与静态兜底清单。
//
// 移植自 workbuddy2api-panel internal/upstream/client.go（FetchModels 族）、
// global_models.go 与 effort_catalog.go。本包 Client 无缓存（无全局状态约束），
// 探测结果由调用方按需缓存；静态兜底表编译期内嵌。
package workbuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// ModelInfo 动态模型信息（本包的精简产出契约：ID + 展示名 + 推理档位）。
type ModelInfo struct {
	ID      string   // 模型 id（出站 model 字段取值）
	Display string   // 展示名（上游 name；缺失回落 ID）
	Efforts []string // reasoning.supportedEfforts（远端缺失时按 realm 静态表兜底）
}

// 模型目录探测 UA 常量。/v3/config 端点对 UA 敏感且**不同 UA 下发不同模型集合**：
//   - IDE UA  → 14 条（含 o4-mini / enhance-1.0 / auto-chat，无 deepseek 系列）
//   - CLI UA  → 22 条（含 deepseek-v4.1-flash / gpt-6-astra / kimi-k2.8-preview）
//   - 桌面三段式 UA → global 侧 29 条（唯一含 gpt-6-sol / gpt-6-luna / grok-4.7 等）
//
// 版本号需随上游 IDE 发版跟进（过旧时该端点可能返回精简目录）。
const (
	codeBuddyIDEUA = "CodeBuddyIDE/4.12.0 CodeBuddy/4.12.0"
	codeBuddyCLIUA = "CLI/2.63.2 CodeBuddy/2.63.2"
)

// dynModelEntry 上游模型目录的单条模型解析形态（CN /console 与 global /v3 同构）。
// id 回退链 id→modelId→model→name（宽松键仅出现在 global 目录变体里）。
type dynModelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// ModelID / Model 仅 global 目录的多信封兜底用，CN 目录不下发这两个键。
	ModelID         string   `json:"modelId"`
	Model           string   `json:"model"`
	Description     string   `json:"descriptionZh"`
	Credits         string   `json:"credits"`
	Tags            []string `json:"tags"`
	Vendor          string   `json:"vendor"`
	IsDefault       bool     `json:"isDefault"`
	MaxInputTokens  int64    `json:"maxInputTokens"`
	MaxOutputTokens int64    `json:"maxOutputTokens"`
	MaxAllowedSize  int64    `json:"maxAllowedSize"`
	Disabled        bool     `json:"disabled"`
	SupportsImages  bool     `json:"supportsImages"`
	SupportsReason  bool     `json:"supportsReasoning"`
	SupportsTool    bool     `json:"supportsToolCall"`
	OnlyReasoning   bool     `json:"onlyReasoning"`
	Reasoning       struct {
		Effort             string   `json:"effort"`
		Summary            string   `json:"summary"`
		DefaultEffort      string   `json:"defaultEffort"`
		CanDisableThinking bool     `json:"canDisableThinking"`
		SupportedEfforts   []string `json:"supportedEfforts"`
	} `json:"reasoning"`
}

// modelInfo 按解析条目构造 ModelInfo（dynEntry→ModelInfo 映射的单一事实来源）。
func (m dynModelEntry) modelInfo() ModelInfo {
	return ModelInfo{
		ID:      m.ID,
		Display: m.Name,
		Efforts: m.Reasoning.SupportedEfforts,
	}
}

// nonChatModel 判定是否非对话模型（应从模型列表过滤掉，选了报 code=11102）。三类规则：
//   - id 前缀 nes-/completion-/codewise-：嵌入/补全/代码专用模型；
//   - maxOutputTokens ≤ 256：tiny 输出非对话模型；
//   - tags 含生成类标签（图片/视频）：生成模型走专用端点。
func nonChatModel(id string, maxOutputTokens int64, tags []string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range [...]string{"nes-", "completion-", "codewise-"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	if maxOutputTokens > 0 && maxOutputTokens <= 256 {
		return true
	}
	for _, t := range tags {
		switch t {
		case "text-to-image", "image-to-image", "text-to-video", "image-to-video":
			return true
		}
	}
	return false
}

// FetchModels 拉取账号所在 realm 的模型目录。CN：/v3/config（IDE UA）与
// /console/enterprises/personal/models（cli 面过滤）**并发**探测取并集，v3 条目
// 优先（字段以 v3 为准），企业端点只补 v3 缺失的模型。global：/v3/config 三种 UA
// （桌面三段式主路 + IDE + CLI）与企业端点家族（/v2 → /console 补缺）并发探测取并集。
// 两路全失败才返回错误（单路失败降级为另一路结果）。
//
// 远端未下发 supportedEfforts 的模型按 realm 静态表兜底（见 fillStaticEfforts）。
func (c *Client) FetchModels(ctx context.Context, cred Credentials) ([]ModelInfo, error) {
	if cred.IsGlobal() {
		return c.fetchGlobalModels(ctx, cred)
	}
	var (
		wg                   sync.WaitGroup
		enterprise, v3       []ModelInfo
		enterpriseErr, v3Err error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		enterprise, enterpriseErr = c.fetchEnterpriseModels(ctx, cred, "/console/enterprises/personal/models")
	}()
	go func() {
		defer wg.Done()
		v3, v3Err = c.fetchV3Models(ctx, cred, codeBuddyIDEUA)
	}()
	wg.Wait()
	if enterpriseErr != nil && v3Err != nil {
		return nil, enterpriseErr // 两路全失败：返回企业端点错误（既有调用方语义）
	}
	out := mergeModelInfos(v3, enterprise)
	if len(out) == 0 {
		return nil, fmt.Errorf("models api returned empty list")
	}
	fillStaticEfforts(RealmCN, out)
	return out, nil
}

// mergeModelInfos 合并两路模型目录：primary 为主（同 id 以 primary 条目为准），
// secondary 只补 primary 缺失的 id。输出顺序 = primary 原序在前、secondary 补充项
// 在后——稳定输出，不依赖 map 迭代序。
func mergeModelInfos(primary, secondary []ModelInfo) []ModelInfo {
	if len(secondary) == 0 {
		return primary
	}
	seen := make(map[string]bool, len(primary)+len(secondary))
	out := make([]ModelInfo, 0, len(primary)+len(secondary))
	for _, mi := range primary {
		if mi.ID == "" || seen[mi.ID] {
			continue
		}
		seen[mi.ID] = true
		out = append(out, mi)
	}
	for _, mi := range secondary {
		if mi.ID == "" || seen[mi.ID] {
			continue
		}
		seen[mi.ID] = true
		out = append(out, mi)
	}
	return out
}

// fetchEnterpriseModels 单路探测企业模型端点。解析口径：agents[cli].models 过滤 +
// nonChatModel 剔除 + disabled 剔除。
func (c *Client) fetchEnterpriseModels(ctx context.Context, cred Credentials, path string) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.chatBase(cred)+path, nil)
	if err != nil {
		return nil, err
	}
	CommonHeaders(req, cred)
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	resp, err := c.httpc().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models api status %d: %s", resp.StatusCode, truncate(string(raw), 120))
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Models []dynModelEntry `json:"models"`
			Agents []struct {
				Name   string   `json:"name"`
				Models []string `json:"models"`
			} `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("models parse: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("models api code=%d", env.Code)
	}
	var cliIDs []string
	for _, ag := range env.Data.Agents {
		if ag.Name == "cli" {
			cliIDs = ag.Models
			break
		}
	}
	// 无 cli 面时退化为全量 models（global 家族端点不下发 agents）。
	byID := make(map[string]dynModelEntry, len(env.Data.Models))
	for _, m := range env.Data.Models {
		id := m.ID
		if id == "" {
			id = m.ModelID
		}
		if id == "" {
			id = m.Model
		}
		if id == "" {
			id = m.Name
		}
		if id == "" || nonChatModel(id, m.MaxOutputTokens, m.Tags) {
			continue
		}
		m.ID = id
		byID[id] = m
	}
	ids := cliIDs
	if len(ids) == 0 {
		ids = make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		sort.Strings(ids) // map 迭代序随机，排序保输出稳定
	}
	out := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok && !m.Disabled {
			out = append(out, m.modelInfo())
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("models api returned empty list")
	}
	return out, nil
}

// fetchV3Models 单路探测 /v3/config（ua 指定出站 UA，见三个 UA 常量注释）。
// v3 面取全量 models（不按 agents[cli] 过滤），按 nonChatModel 规则剔除。
func (c *Client) fetchV3Models(ctx context.Context, cred Credentials, ua string) ([]ModelInfo, error) {
	byID, err := c.fetchV3ConfigModelMap(ctx, cred, ua)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(byID))
	for _, mi := range byID {
		out = append(out, mi)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("v3/config returned empty models")
	}
	return out, nil
}

// fetchV3ConfigModelMap 拉官方 IDE 配置目录，按模型 id 建表。
// 该端点对 UA 敏感：必须带 CodeBuddy/CodeBuddyIDE 版本，否则 400 code=12403。
// 同时解析试用横幅（ModelTrialBanner）：上游把「N 天免费试用」的模型只放在这里、
// data.models 里没有，但该模型**实际可调用**——能力字段从 targetModelId 继承。
func (c *Client) fetchV3ConfigModelMap(ctx context.Context, cred Credentials, ua string) (map[string]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.chatBase(cred)+"/v3/config", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	if cred.UID != "" {
		req.Header.Set("X-User-Id", cred.UID)
	}
	req.Header.Set("X-Domain", v3ConfigDomain(cred, c.chatBase(cred)))
	req.Header.Set("X-Product", "SaaS")
	req.Header.Set("User-Agent", ua)
	req.Header.Set("X-CodeBuddy-Request", "1")
	resp, err := c.httpc().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("v3/config status %d: %s", resp.StatusCode, truncate(string(raw), 120))
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Models []dynModelEntry `json:"models"`
			// 试用模型横幅：modelId（试用版）/ targetModelId（转正目标）。
			ProductFeaturesConfig struct {
				ModelTrialBanner struct {
					Banners []struct {
						ModelID       string `json:"modelId"`
						TargetModelID string `json:"targetModelId"`
					} `json:"banners"`
				} `json:"ModelTrialBanner"`
			} `json:"productFeaturesConfig"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("v3/config parse: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("v3/config code=%d", env.Code)
	}
	out := make(map[string]ModelInfo, len(env.Data.Models))
	for _, m := range env.Data.Models {
		if strings.TrimSpace(m.ID) == "" || nonChatModel(m.ID, m.MaxOutputTokens, m.Tags) {
			continue
		}
		out[m.ID] = m.modelInfo()
	}
	// 补入试用横幅模型：能力字段从 targetModelId 继承（同族模型能力一致）。
	for _, b := range env.Data.ProductFeaturesConfig.ModelTrialBanner.Banners {
		id := strings.TrimSpace(b.ModelID)
		if id == "" {
			continue
		}
		if _, exists := out[id]; exists {
			continue
		}
		mi := ModelInfo{ID: id, Display: id}
		if tgt := strings.TrimSpace(b.TargetModelID); tgt != "" {
			if base, ok := out[tgt]; ok {
				mi = base
				mi.ID = id
			}
		}
		out[id] = mi
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("v3/config returned empty models")
	}
	return out, nil
}

// v3ConfigDomain /v3/config 的 X-Domain：优先账号 domain，否则 chatBase host。
func v3ConfigDomain(cred Credentials, chatBase string) string {
	if d := strings.TrimSpace(cred.Domain); d != "" {
		d = strings.TrimPrefix(d, "https://")
		d = strings.TrimPrefix(d, "http://")
		return strings.TrimSuffix(d, "/")
	}
	if u, err := url.Parse(chatBase); err == nil && u.Host != "" {
		return u.Host
	}
	return "copilot.tencent.com"
}

// globalModelsProbePaths global 企业模型目录端点候选序列：/v2 家族优先（实测
// /v2/enterprises/personal/models 200 含完整模型表），/console 作 fallback。
var globalModelsProbePaths = []string{
	"/v2/enterprises/personal/models",
	"/console/enterprises/personal/models",
}

// fetchGlobalModels global realm 模型目录探测：/v3/config 三种 UA（桌面三段式主路
// 字段权威 + IDE + CLI 补缺）与企业端点家族并发，并集合并 + nonChatModel 剔除。
// 桌面 UA 是主路：它是 chat 路径实际使用的 UA，与客户端看到的目录同源。
func (c *Client) fetchGlobalModels(ctx context.Context, cred Credentials) ([]ModelInfo, error) {
	type probeResult struct {
		infos []ModelInfo
		err   error
	}
	run := func(fn func() ([]ModelInfo, error)) chan probeResult {
		ch := make(chan probeResult, 1)
		go func() {
			infos, err := fn()
			ch <- probeResult{infos, err}
		}()
		return ch
	}
	v3DesktopCh := run(func() ([]ModelInfo, error) { return c.fetchV3Models(ctx, cred, defaultWorkBuddyUA(cred)) })
	v3IDECh := run(func() ([]ModelInfo, error) { return c.fetchV3Models(ctx, cred, codeBuddyIDEUA) })
	v3CLICh := run(func() ([]ModelInfo, error) { return c.fetchV3Models(ctx, cred, codeBuddyCLIUA) })
	enterpriseCh := run(func() ([]ModelInfo, error) {
		var lastErr error
		for _, path := range globalModelsProbePaths {
			infos, err := c.fetchEnterpriseModels(ctx, cred, path)
			if err != nil {
				lastErr = err
				continue
			}
			return infos, nil
		}
		return nil, lastErr
	})
	v3Desktop, v3IDE, v3CLI, enterprise := <-v3DesktopCh, <-v3IDECh, <-v3CLICh, <-enterpriseCh

	// v3 三路合并：按权威顺序（桌面 UA 主路在前）取并集，先成功路为基底（字段权威），
	// 后续成功路只补缺失 id；任一路成功即 v3 可用，全失败才记录错误。
	var v3Infos []ModelInfo
	var v3Err error
	nOK := 0
	for _, r := range []probeResult{v3Desktop, v3IDE, v3CLI} {
		if r.err != nil {
			if v3Err == nil {
				v3Err = r.err
			}
			continue
		}
		nOK++
		v3Infos = mergeModelInfos(v3Infos, r.infos)
	}
	if nOK > 0 {
		v3Err = nil
	}
	if v3Err != nil && enterprise.err != nil {
		return nil, v3Err // 两路全失败
	}
	// 两路皆成功（或单路降级）：v3 为主、企业端点补缺。
	out := mergeModelInfos(v3Infos, enterprise.infos)
	if len(out) == 0 {
		return nil, fmt.Errorf("global models empty list")
	}
	fillStaticEfforts(RealmGlobal, out)
	return out, nil
}

// fillStaticEfforts 就地补齐 Efforts 缺失条目：远端未下发 supportedEfforts 的模型
// 按 realm 静态兜底表填（cn/global 两表刻意不同，如 deepseek-v4.1-flash CN 三档、
// global 仅 high）。仍无的保持 nil（调用方按「无档位」处理）。
func fillStaticEfforts(realm string, out []ModelInfo) {
	for i := range out {
		if len(out[i].Efforts) > 0 {
			continue
		}
		if cap, ok := staticEffortCap(realm, out[i].ID); ok {
			out[i].Efforts = cap.efforts
		}
		if out[i].Display == "" {
			out[i].Display = out[i].ID
		}
	}
}

// ---------------------------------------------------------------------------
// 静态兜底清单（从源项目 effort_catalog.go / global_models.go 逐字提取）。
// ---------------------------------------------------------------------------

// effortCap 一个模型的档位能力（efforts + 默认档）。
type effortCap struct {
	efforts       []string
	defaultEffort string
}

// cnEffortFallback CN / CodeBuddy 面静态兜底表。
// 三档模型 defaultEffort 均为 high。
var cnEffortFallback = map[string]effortCap{
	"deepseek-v4-flash":   {efforts: []string{"low", "high", "max"}},
	"deepseek-v4.1-flash": {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"deepseek-v4-pro":     {efforts: []string{"low", "high", "xhigh"}, defaultEffort: "high"},
	"hy4-preview":         {efforts: []string{"high"}, defaultEffort: "high"},
	"hy4-preview-x":       {efforts: []string{"high"}},
	"hy3":                 {efforts: []string{"low", "high"}, defaultEffort: "high"},
	"hy3-x":               {efforts: []string{"low", "high"}, defaultEffort: "high"},
	"glm-5.3":             {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"glm-5.3-flash":       {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"glm-5.2":             {efforts: []string{"high", "xhigh"}, defaultEffort: "high"},
	"glm-5.1":             {efforts: []string{"medium"}},
	"glm-5v-turbo":        {efforts: []string{"medium"}},
	"kimi-k3-1":           {efforts: []string{"medium"}},
	"kimi-k2.7":           {efforts: []string{"medium"}},
	"kimi-k2.6":           {efforts: []string{"medium"}},
	"minimax-m3":          {efforts: []string{"medium"}},
}

// globalEffortFallback global / WorkBuddy 国际版面静态兜底表。
// 注意 deepseek-v4.1-flash 在国际版**只有 ['high']**（实测 IDE 缓存），与 CN 面的
// 三档刻意不同——往 WorkBuddy 上游发 low/max 是非法参数 400。
var globalEffortFallback = map[string]effortCap{
	"fast-model":          {efforts: []string{"medium"}},
	"balanced-model":      {efforts: []string{"medium"}},
	"primary-model":       {efforts: []string{"high"}},
	"hy4-preview-f":       {efforts: []string{"high"}, defaultEffort: "high"},
	"hy3":                 {efforts: []string{"low", "high"}, defaultEffort: "high"},
	"deepseek-v4.1-flash": {efforts: []string{"high"}},
	"gpt-6-astra":         {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.6-sol":         {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.6-terra":       {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.6-luna":        {efforts: []string{"low", "medium", "high", "xhigh", "max"}, defaultEffort: "high"},
	"gpt-5.5":             {efforts: []string{"low", "medium", "high", "xhigh"}, defaultEffort: "high"},
	"gpt-5.4":             {efforts: []string{"low", "medium", "high", "xhigh"}, defaultEffort: "high"},
	"gpt-5.3-codex":       {efforts: []string{"medium"}},
	"gemini-3.5-flash":    {efforts: []string{"medium"}},
	"glm-5.3":             {efforts: []string{"low", "high", "max"}, defaultEffort: "high"},
	"glm-5.2":             {efforts: []string{"high", "xhigh"}, defaultEffort: "high"},
	"kimi-k3":             {efforts: []string{"medium"}},
	"kimi-k2.6":           {efforts: []string{"medium"}},
}

// GlobalModelNames 国际版历史静态名单（源项目 PLAN §7.2 附录 21 名）。
// 纯动态化后不再作为目录兜底（拉不出目录即上游不可用，假名单只会让客户端选到
// 11102 的模型），保留作对照与离线展示兜底。
var GlobalModelNames = []string{
	"default-model",
	"fast-model",
	"balanced-model",
	"primary-model",
	"hy4-preview",
	"gpt-5.6-sol",
	"gpt-5.6-terra",
	"deep-model",
	"deepseek-v4.1-flash",
	"gpt-6-astra",
	"hy4-preview-f",
	"hy3",
	"glm-5.2",
	"gpt-5.6-luna",
	"gpt-5.5",
	"gpt-5.4",
	"gpt-5.3-codex",
	"gemini-3.5-flash",
	"glm-5.3",
	"kimi-k3",
	"kimi-k2.6",
}

// staticEffortCap 按 realm 取静态兜底条目；未命中返回 zero（efforts=nil）。
func staticEffortCap(realm, model string) (effortCap, bool) {
	table := cnEffortFallback
	if realm == RealmGlobal {
		table = globalEffortFallback
	}
	cap, ok := table[model]
	return cap, ok
}

// FallbackModels 静态兜底模型清单（上游目录不可达时的离线名单）：
// CN 取 cnEffortFallback 全表，global 取 GlobalModelNames（按 effort 表补档位）。
// 注意：静态名单可能滞后于上游（新模型不在其中），仅作兜底展示。
func FallbackModels(realm string) []ModelInfo {
	if realm == RealmGlobal {
		out := make([]ModelInfo, 0, len(GlobalModelNames))
		for _, id := range GlobalModelNames {
			mi := ModelInfo{ID: id, Display: id}
			if cap, ok := staticEffortCap(realm, id); ok {
				mi.Efforts = cap.efforts
			}
			out = append(out, mi)
		}
		return out
	}
	ids := make([]string, 0, len(cnEffortFallback))
	for id := range cnEffortFallback {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		mi := ModelInfo{ID: id, Display: id, Efforts: cnEffortFallback[id].efforts}
		out = append(out, mi)
	}
	return out
}

// ---------------------------------------------------------------------------
// effort 档位降级（chat 出站路径用，源项目 payload.go normalizeReasoningEffort）。
// ---------------------------------------------------------------------------

// effortRank 档位从低到高。
var effortRank = map[string]int{"off": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6}

// normalizeReasoningEffort 按模型 supportedEfforts 降级 reasoning_effort
// （snake/camel 双字段兼容）：
//   - 请求档位模型支持 → 原样透传；
//   - 请求档位不支持 → 改为 ≤请求档位的最高支持档（降级）；
//   - 支持档全部高于请求档 → 取最低支持档（偏离最小）；
//   - 未知模型/未知档位/未携带字段 → 一律透传。
func normalizeReasoningEffort(obj map[string]any, efforts map[string][]string) {
	if len(efforts) == 0 {
		return
	}
	model, _ := obj["model"].(string)
	if model == "" {
		return
	}
	supported, ok := efforts[model]
	if !ok || len(supported) == 0 {
		return
	}
	key := ""
	if _, present := obj["reasoning_effort"]; present {
		key = "reasoning_effort"
	} else if _, present := obj["reasoningEffort"]; present {
		key = "reasoningEffort"
	} else {
		return
	}
	reqStr, ok := obj[key].(string)
	if !ok {
		return
	}
	reqStr = strings.TrimSpace(strings.ToLower(reqStr))
	reqIdx, known := effortRank[reqStr]
	if !known {
		return
	}
	// 在 ≤请求档位的支持档里选最高档；命中且与请求不同才改写。
	best, bestIdx := "", -1
	for _, s := range supported {
		idx, k := effortRank[strings.TrimSpace(strings.ToLower(s))]
		if k && idx <= reqIdx && idx > bestIdx {
			best, bestIdx = s, idx
		}
	}
	if best != "" {
		if !strings.EqualFold(best, reqStr) {
			obj[key] = best
		}
		return
	}
	// 支持档全部高于请求档：取最低支持档。
	lowest, lowestIdx := "", 1<<30
	for _, s := range supported {
		idx, k := effortRank[strings.TrimSpace(strings.ToLower(s))]
		if k && idx < lowestIdx {
			lowest, lowestIdx = s, idx
		}
	}
	if lowest != "" {
		obj[key] = lowest
	}
}

// globalEffortMap global 域降级用的 effort 能力表：静态兜底表为基，远端桶覆盖
// （权威优先）。global 上游可能不下发 supportedEfforts——此时也必须按产品静态表
// 降级（如 deepseek-v4.1-flash 国际版只认 high，客户端传 low/max 必降级到 high，
// 否则上游 400 毁掉请求）。remoteEfforts/remoteDefaults 为 nil 时纯静态表。
func globalEffortMap(remoteEfforts map[string][]string, remoteDefaults map[string]string) (map[string][]string, map[string]string) {
	efforts := make(map[string][]string, len(globalEffortFallback)+len(remoteEfforts))
	defs := make(map[string]string, len(globalEffortFallback)+len(remoteDefaults))
	for id, cap := range globalEffortFallback {
		efforts[id] = append([]string(nil), cap.efforts...)
		if cap.defaultEffort != "" {
			defs[id] = cap.defaultEffort
		}
	}
	for id, v := range remoteEfforts {
		if len(v) > 0 {
			efforts[id] = v
		}
	}
	for id, v := range remoteDefaults {
		if v != "" {
			defs[id] = v
		}
	}
	return efforts, defs
}
