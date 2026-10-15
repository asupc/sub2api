//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

// fakeModelManifestSource 模型清单测试用的窄数据源
type fakeModelManifestSource struct {
	accounts []Account
}

func (f *fakeModelManifestSource) ListAllWithFilters(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, error) {
	return f.accounts, nil
}

func modelManifestAccount(id int64, name, platform, accountType string) *Account {
	return &Account{
		ID:          id,
		Name:        name,
		Platform:    platform,
		Type:        accountType,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 5,
		Priority:    10,
		Groups:      []*Group{{ID: 1, Name: "VIP分组"}, {ID: 2, Name: "默认分组"}},
	}
}

func TestModelManifest_MappingPriority(t *testing.T) {
	// 同一账号同时配置映射与同步元数据：映射 keys 优先，元数据被忽略
	account := modelManifestAccount(1, "acc-1", PlatformAnthropic, AccountTypeOAuth)
	account.Credentials = map[string]any{
		"model_mapping": map[string]any{
			"claude-custom": "claude-sonnet-5",
		},
	}
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{
			"metadata-only-model": {ID: "metadata-only-model"},
		},
	})

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*account}})
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{})
	require.NoError(t, err)

	require.Len(t, manifest.Models, 1)
	model := manifest.Models[0]
	require.Equal(t, "claude-custom", model.ID)
	require.Equal(t, "claude-sonnet-5", model.Accounts[0].Target)
	require.Equal(t, 1, model.ActiveAccounts)
	require.False(t, model.HasWarning)
}

func TestModelManifest_MetadataFallback(t *testing.T) {
	// 无映射但有同步元数据：元数据 keys 生效
	account := modelManifestAccount(1, "acc-1", PlatformOpenAI, AccountTypeOAuth)
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{
			"gpt-synced": {ID: "gpt-synced", DisplayName: "GPT Synced"},
		},
	})

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*account}})
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{})
	require.NoError(t, err)

	require.Len(t, manifest.Models, 1)
	require.Equal(t, "gpt-synced", manifest.Models[0].ID)
	require.Equal(t, "GPT Synced", manifest.Models[0].DisplayName)
	require.Empty(t, manifest.Models[0].Accounts[0].Target)
}

func TestModelManifest_PlatformDefaultFallback(t *testing.T) {
	// 无映射且无元数据：回落平台默认清单（anthropic → claude.DefaultModels）
	account := modelManifestAccount(1, "acc-1", PlatformAnthropic, AccountTypeOAuth)

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*account}})
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{})
	require.NoError(t, err)

	require.NotEmpty(t, manifest.Models)
	require.Equal(t, len(claudeDefaultModelIDsForTest()), len(manifest.Models))
	ids := make([]string, 0, len(manifest.Models))
	for _, m := range manifest.Models {
		ids = append(ids, m.ID)
	}
	require.Contains(t, ids, claudeDefaultModelIDsForTest()[0])
}

func TestModelManifest_DangerWarning(t *testing.T) {
	// 模型下唯一账号被限流：可调度账号 = 0 → 断供告警
	resetAt := time.Now().Add(10 * time.Minute)
	rateLimited := modelManifestAccount(1, "acc-rl", PlatformOpenAI, AccountTypeAPIKey)
	rateLimited.Credentials = map[string]any{
		"model_mapping": map[string]any{"gpt-passthrough": "gpt-5.5"},
	}
	rateLimited.RateLimitResetAt = &resetAt

	// 另一个健康账号提供别的模型
	healthy := modelManifestAccount(2, "acc-ok", PlatformOpenAI, AccountTypeAPIKey)
	healthy.Credentials = map[string]any{
		"model_mapping": map[string]any{"gpt-healthy": "gpt-5.5"},
	}

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*rateLimited, *healthy}})
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{})
	require.NoError(t, err)

	byID := make(map[string]ModelManifestItem)
	for _, m := range manifest.Models {
		byID[m.ID] = m
	}
	require.True(t, byID["gpt-passthrough"].HasWarning)
	require.Equal(t, 0, byID["gpt-passthrough"].ActiveAccounts)
	require.Equal(t, 1, byID["gpt-passthrough"].TotalAccounts)
	require.True(t, byID["gpt-passthrough"].Accounts[0].RateLimited)
	require.False(t, byID["gpt-passthrough"].Accounts[0].Schedulable)
	require.False(t, byID["gpt-healthy"].HasWarning)
	require.Equal(t, 5, byID["gpt-healthy"].TotalConcurrency)

	require.Equal(t, 2, manifest.Summary.TotalModels)
	require.Equal(t, 1, manifest.Summary.HealthyModels)
	require.Equal(t, 1, manifest.Summary.DangerModels)
	require.Equal(t, 2, manifest.Summary.TotalAccounts)
}

func TestModelManifest_MultiPlatformAggregation(t *testing.T) {
	// 同一模型 ID 由两个平台的账号同时供给：Platforms 聚合、账号去重
	claudeAcc := modelManifestAccount(1, "claude-acc", PlatformAnthropic, AccountTypeOAuth)
	claudeAcc.Credentials = map[string]any{
		"model_mapping": map[string]any{"shared-model": "claude-sonnet-5"},
	}
	agAcc := modelManifestAccount(2, "ag-acc", PlatformAntigravity, AccountTypeOAuth)
	agAcc.Credentials = map[string]any{
		"model_mapping": map[string]any{"shared-model": "gemini-3-pro-preview"},
	}

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*claudeAcc, *agAcc}})
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{})
	require.NoError(t, err)

	var shared ModelManifestItem
	found := false
	for _, m := range manifest.Models {
		if m.ID == "shared-model" {
			shared = m
			found = true
		}
	}
	require.True(t, found)
	require.Equal(t, []string{PlatformAnthropic, PlatformAntigravity}, shared.Platforms)
	require.Equal(t, 2, shared.TotalAccounts)
	require.ElementsMatch(t, []string{"VIP分组", "默认分组"}, shared.Accounts[0].Groups)
}

func TestModelManifest_Filters(t *testing.T) {
	claudeAcc := modelManifestAccount(1, "claude-acc", PlatformAnthropic, AccountTypeOAuth)
	claudeAcc.Credentials = map[string]any{
		"model_mapping": map[string]any{"claude-alpha": "claude-sonnet-5"},
	}
	openaiAcc := modelManifestAccount(2, "openai-acc", PlatformOpenAI, AccountTypeAPIKey)
	openaiAcc.Credentials = map[string]any{
		"model_mapping": map[string]any{"gpt-beta": "gpt-5.5"},
	}
	disabled := modelManifestAccount(3, "dead-acc", PlatformOpenAI, AccountTypeAPIKey)
	disabled.Status = "inactive"
	disabled.Schedulable = false
	disabled.Credentials = map[string]any{
		"model_mapping": map[string]any{"gpt-dead": "gpt-5.5"},
	}

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*claudeAcc, *openaiAcc, *disabled}})

	// 平台过滤
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{Platform: PlatformAnthropic})
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	require.Equal(t, "claude-alpha", manifest.Models[0].ID)
	// summary 不受过滤影响
	require.Equal(t, 3, manifest.Summary.TotalModels)

	// 状态过滤：danger = 可调度账号为 0
	manifest, err = svc.GetManifest(context.Background(), ModelManifestQuery{Status: "danger"})
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	require.Equal(t, "gpt-dead", manifest.Models[0].ID)

	// 状态过滤：healthy
	manifest, err = svc.GetManifest(context.Background(), ModelManifestQuery{Status: "healthy"})
	require.NoError(t, err)
	require.Len(t, manifest.Models, 2)

	// 搜索：按模型 ID
	manifest, err = svc.GetManifest(context.Background(), ModelManifestQuery{Search: "gpt-beta"})
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	require.Equal(t, "gpt-beta", manifest.Models[0].ID)

	// 搜索：按挂载账号名
	manifest, err = svc.GetManifest(context.Background(), ModelManifestQuery{Search: "claude-acc"})
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	require.Equal(t, "claude-alpha", manifest.Models[0].ID)
}

func TestModelManifest_WildcardMappingSkipped(t *testing.T) {
	// 通配符映射无法枚举模型，不应出现在清单中
	account := modelManifestAccount(1, "wild-acc", PlatformOpenAI, AccountTypeAPIKey)
	account.Credentials = map[string]any{
		"model_mapping": map[string]any{"*": "gpt-5.5"},
	}

	svc := NewModelManifestService(&fakeModelManifestSource{accounts: []Account{*account}})
	manifest, err := svc.GetManifest(context.Background(), ModelManifestQuery{})
	require.NoError(t, err)

	for _, m := range manifest.Models {
		require.NotContains(t, m.ID, "*")
	}
}

// claudeDefaultModelIDsForTest 返回 claude 默认模型 ID 列表（测试断言用）
func claudeDefaultModelIDsForTest() []string {
	ids := make([]string, 0, len(claude.DefaultModels))
	for _, m := range claude.DefaultModels {
		ids = append(ids, m.ID)
	}
	return ids
}
