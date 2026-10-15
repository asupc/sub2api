package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// ModelManifestAccountSource 是模型清单聚合所需的窄账号数据源：
// 只需全量列举（软删除自动排除），刻意窄于 AccountRepository，
// 单元测试无需实现整个仓储接口。
type ModelManifestAccountSource interface {
	// ListAllWithFilters 返回符合过滤条件的全部账号（不分页）。
	ListAllWithFilters(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, error)
}

// ModelManifestService 模型清单与账号供给透视服务：一次 DB 全量加载后在内存中
// 构建「模型 → 挂载账号」倒排索引，0 次上游网络请求。
type ModelManifestService struct {
	accounts ModelManifestAccountSource
}

// NewModelManifestService 创建模型清单服务实例
func NewModelManifestService(accounts ModelManifestAccountSource) *ModelManifestService {
	return &ModelManifestService{accounts: accounts}
}

// ModelManifestQuery 模型清单查询参数
type ModelManifestQuery struct {
	Platform string // 可选：按平台过滤（账号平台 ID）
	Status   string // 可选：all / healthy / danger
	Search   string // 可选：模型 ID、显示名或账号名模糊匹配
}

// ModelManifestSummary 全盘供给核心指标（不受查询过滤影响，始终为全局口径）
type ModelManifestSummary struct {
	TotalModels   int `json:"total_models"`
	HealthyModels int `json:"healthy_models"`
	DangerModels  int `json:"danger_models"`
	TotalAccounts int `json:"total_accounts"`
}

// ModelManifestAccountItem 模型下挂载账号的明细
type ModelManifestAccountItem struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Platform    string   `json:"platform"`
	Type        string   `json:"type"`
	Status      string   `json:"status"`
	Schedulable bool     `json:"schedulable"`
	RateLimited bool     `json:"rate_limited"`
	Concurrency int      `json:"concurrency"`
	Priority    int      `json:"priority"`
	Groups      []string `json:"groups"`
	Target      string   `json:"target"` // 映射目标；空表示原生直通
}

// ModelManifestItem 单个模型的供给画像
type ModelManifestItem struct {
	ID               string                     `json:"id"`
	DisplayName      string                     `json:"display_name"`
	Platforms        []string                   `json:"platforms"`
	TotalAccounts    int                        `json:"total_accounts"`
	ActiveAccounts   int                        `json:"active_accounts"`
	TotalConcurrency int                        `json:"total_concurrency"`
	HasWarning       bool                       `json:"has_warning"` // true = 0 可调度账号
	Accounts         []ModelManifestAccountItem `json:"accounts"`
}

// ModelManifestResponse 模型清单响应
type ModelManifestResponse struct {
	Summary ModelManifestSummary `json:"summary"`
	Models  []ModelManifestItem  `json:"models"`
}

// modelManifestCollector 倒排索引构建器：modelID → 模型条目
type modelManifestCollector struct {
	items        map[string]*ModelManifestItem
	platformSets map[string]map[string]struct{}
	accountsSeen map[int64]struct{}
}

func newModelManifestCollector() *modelManifestCollector {
	return &modelManifestCollector{
		items:        make(map[string]*ModelManifestItem),
		platformSets: make(map[string]map[string]struct{}),
		accountsSeen: make(map[int64]struct{}),
	}
}

func (col *modelManifestCollector) model(modelID, displayName string) *ModelManifestItem {
	item, ok := col.items[modelID]
	if !ok {
		item = &ModelManifestItem{ID: modelID, DisplayName: displayName}
		col.items[modelID] = item
		col.platformSets[modelID] = make(map[string]struct{})
	}
	if item.DisplayName == "" && displayName != "" {
		item.DisplayName = displayName
	}
	return item
}

func (col *modelManifestCollector) addAccount(account *Account, modelID, displayName, target string) {
	item := col.model(modelID, displayName)
	if _, seen := col.platformSets[modelID][account.Platform]; !seen {
		col.platformSets[modelID][account.Platform] = struct{}{}
	}

	// 同一模型下同一账号只记一次（映射与元数据可能同时命中）
	for _, existing := range item.Accounts {
		if existing.ID == account.ID {
			return
		}
	}

	groups := make([]string, 0, len(account.Groups))
	for _, g := range account.Groups {
		if g != nil && strings.TrimSpace(g.Name) != "" {
			groups = append(groups, g.Name)
		}
	}
	sort.Strings(groups)

	item.Accounts = append(item.Accounts, ModelManifestAccountItem{
		ID:          account.ID,
		Name:        account.Name,
		Platform:    account.Platform,
		Type:        account.Type,
		Status:      account.Status,
		Schedulable: account.IsSchedulable(),
		RateLimited: account.IsRateLimited(),
		Concurrency: account.Concurrency,
		Priority:    account.Priority,
		Groups:      groups,
		Target:      target,
	})
	item.TotalAccounts++
	if account.IsSchedulable() {
		item.ActiveAccounts++
		if account.Concurrency > 0 {
			item.TotalConcurrency += account.Concurrency
		}
	}
	item.HasWarning = item.ActiveAccounts == 0
	col.accountsSeen[account.ID] = struct{}{}
}

func (col *modelManifestCollector) build() []ModelManifestItem {
	models := make([]ModelManifestItem, 0, len(col.items))
	for modelID, item := range col.items {
		model := *item
		platforms := make([]string, 0, len(col.platformSets[modelID]))
		for platform := range col.platformSets[modelID] {
			platforms = append(platforms, platform)
		}
		sort.Strings(platforms)
		model.Platforms = platforms
		sort.SliceStable(model.Accounts, func(i, j int) bool {
			if model.Accounts[i].Priority != model.Accounts[j].Priority {
				return model.Accounts[i].Priority > model.Accounts[j].Priority
			}
			return model.Accounts[i].ID < model.Accounts[j].ID
		})
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

// GetManifest 构建模型清单：全量加载未删除账号 → 内存倒排聚合 → 过滤返回。
func (s *ModelManifestService) GetManifest(ctx context.Context, query ModelManifestQuery) (*ModelManifestResponse, error) {
	accounts, err := s.accounts.ListAllWithFilters(ctx, "", "", "", "", 0, "")
	if err != nil {
		return nil, err
	}

	collector := newModelManifestCollector()
	for i := range accounts {
		account := &accounts[i]
		manifestAccountModels(collector, account)
	}

	allModels := collector.build()

	summary := ModelManifestSummary{
		TotalModels:   len(allModels),
		HealthyModels: 0,
		DangerModels:  0,
		TotalAccounts: len(collector.accountsSeen),
	}
	for _, model := range allModels {
		if model.HasWarning {
			summary.DangerModels++
		} else {
			summary.HealthyModels++
		}
	}

	return &ModelManifestResponse{
		Summary: summary,
		Models:  filterModelManifestItems(allModels, query),
	}, nil
}

// manifestAccountModels 解析单个账号对外提供的模型并写入倒排索引。
// 模型解析优先级（由高到低）：显式 model_mapping → 上游同步元数据 → 平台默认清单。
func manifestAccountModels(collector *modelManifestCollector, account *Account) {
	models, source := resolveAccountProvidedModels(account)
	for _, entry := range models {
		target := ""
		if source == modelSourceMapping {
			target = entry.target
		}
		collector.addAccount(account, entry.id, entry.displayName, target)
	}
}

type modelManifestEntry struct {
	id          string
	displayName string
	target      string // 仅 modelSourceMapping 来源有效
}

const (
	modelSourceMapping  = "mapping"
	modelSourceMetadata = "metadata"
	modelSourceDefault  = "default"
)

// resolveAccountProvidedModels 返回账号提供的模型列表及其解析来源。
// 同一优先级内去重排序；任一高优先级来源命中即不再回落。
func resolveAccountProvidedModels(account *Account) ([]modelManifestEntry, string) {
	if mapping := account.GetModelMapping(); len(mapping) > 0 {
		entries := make([]modelManifestEntry, 0, len(mapping))
		for requested, target := range mapping {
			requested = strings.TrimSpace(requested)
			// 通配符映射无法枚举具体模型，跳过
			if requested == "" || strings.Contains(requested, "*") {
				continue
			}
			entries = append(entries, modelManifestEntry{
				id:     requested,
				target: strings.TrimSpace(target),
			})
		}
		if len(entries) > 0 {
			return entries, modelSourceMapping
		}
	}

	if snapshot := account.GetUpstreamModelMetadataSnapshot(); snapshot != nil {
		entries := make([]modelManifestEntry, 0, len(snapshot.Models))
		for modelID, metadata := range snapshot.Models {
			modelID = strings.TrimSpace(modelID)
			if modelID == "" {
				continue
			}
			entries = append(entries, modelManifestEntry{
				id:          modelID,
				displayName: strings.TrimSpace(metadata.DisplayName),
			})
		}
		if len(entries) > 0 {
			return entries, modelSourceMetadata
		}
	}

	return defaultPlatformModelEntries(account), modelSourceDefault
}

// defaultPlatformModelEntries 平台原生默认模型清单兜底（无映射且无同步元数据时）。
func defaultPlatformModelEntries(account *Account) []modelManifestEntry {
	var ids []string
	displayNames := make(map[string]string)

	switch {
	case account.IsAnthropic():
		for _, m := range claude.DefaultModels {
			ids = append(ids, m.ID)
			displayNames[m.ID] = m.DisplayName
		}
	case account.IsOpenAI() || account.IsMultiProtocolAPIKey():
		for _, m := range openai.DefaultModels {
			ids = append(ids, m.ID)
			displayNames[m.ID] = m.DisplayName
		}
	case account.IsGemini():
		defaults := geminicli.DefaultModels
		if account.IsGeminiGoogleOne() {
			defaults = geminicli.GoogleOneModels
		}
		for _, m := range defaults {
			ids = append(ids, m.ID)
			displayNames[m.ID] = m.DisplayName
		}
	case account.Platform == PlatformAntigravity:
		for from := range domain.DefaultAntigravityModelMapping {
			ids = append(ids, from)
		}
	case account.Platform == PlatformWorkbuddy, account.Platform == PlatformQoder, account.Platform == PlatformTrae:
		ids = FallbackIntegratedPlatformModelIDs(account.Platform, integratedPlatformRealm(account))
	}

	entries := make([]modelManifestEntry, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		entries = append(entries, modelManifestEntry{id: id, displayName: displayNames[id]})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
	return entries
}

// integratedPlatformRealm 返回三平台（workbuddy/qoder/trae）账号的区域标记，
// 供静态兜底清单选路；凭据缺失时回落各自默认区域。
func integratedPlatformRealm(account *Account) string {
	switch account.Platform {
	case PlatformWorkbuddy:
		if cred, err := parseWorkbuddyCredentials(account); err == nil {
			return cred.RealmNormalized()
		}
		return ""
	case PlatformQoder:
		return qoderAccountRealm(account)
	default:
		return ""
	}
}

// filterModelManifestItems 按 platform / status / search 过滤模型条目。
func filterModelManifestItems(models []ModelManifestItem, query ModelManifestQuery) []ModelManifestItem {
	platform := strings.TrimSpace(query.Platform)
	status := strings.TrimSpace(strings.ToLower(query.Status))
	search := strings.ToLower(strings.TrimSpace(query.Search))

	filtered := make([]ModelManifestItem, 0, len(models))
	for _, model := range models {
		if platform != "" && !containsString(model.Platforms, platform) {
			continue
		}
		switch status {
		case "healthy":
			if model.HasWarning {
				continue
			}
		case "danger":
			if !model.HasWarning {
				continue
			}
		}
		if search != "" && !modelManifestMatchesSearch(model, search) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered
}

func modelManifestMatchesSearch(model ModelManifestItem, search string) bool {
	if strings.Contains(strings.ToLower(model.ID), search) {
		return true
	}
	if strings.Contains(strings.ToLower(model.DisplayName), search) {
		return true
	}
	for _, account := range model.Accounts {
		if strings.Contains(strings.ToLower(account.Name), search) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
