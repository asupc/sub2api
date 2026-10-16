package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type compositeOwnershipAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r *compositeOwnershipAccountRepo) ListModelAvailabilityCandidates(context.Context, *int64, []string, bool) ([]Account, error) {
	return r.accounts, nil
}

// Scenario: 唯一平台的精确别名可路由
func TestResolveCompositeModelOwnershipKeepsProviderAccountsIsolated(t *testing.T) {
	groupID := int64(7)
	repo := &compositeOwnershipAccountRepo{
		accounts: []Account{
			{
				ID:       1,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-public": "gpt-5"},
				},
			},
			{
				ID:       2,
				Platform: PlatformDeepseek,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"reasoning-alias": "deepseek-v4-pro"},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	deepSeekOwnership, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "reasoning-alias")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatforms: []string{PlatformDeepseek}, Matched: true}, deepSeekOwnership)

	openAIOwnership, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "gpt-public")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatforms: []string{PlatformOpenAI}, Matched: true}, openAIOwnership)
}

// Scenario: 通配符和空映射不声明所有权
func TestResolveCompositeModelOwnershipRequiresNonEmptyExactMappings(t *testing.T) {
	groupID := int64(7)
	repo := &compositeOwnershipAccountRepo{
		accounts: []Account{
			{
				ID:       1,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"*": "gpt-5", "gpt-*": "gpt-5", "empty-alias": ""},
				},
			},
			{
				ID:       2,
				Platform: PlatformGrok,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"grok-public": "grok-4"},
				},
			},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	for _, model := range []string{"gpt-5", "empty-alias", "unknown-alias"} {
		ownership, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, model)
		require.NoError(t, err)
		require.Equal(t, CompositeModelOwnership{}, ownership, "model=%s", model)
	}

	ownership, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "grok-public")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatforms: []string{PlatformGrok}, Matched: true}, ownership)
}

// Scenario: 同平台多账号共享别名 = 单平台所有权；同族多平台别名 = 平台集合
//（族同构与合并调度由 Resolve 判定，所有权解析只负责如实上报认领平台）。
func TestResolveCompositeModelOwnershipAllowsSamePlatformAndReturnsCrossPlatformSet(t *testing.T) {
	groupID := int64(7)
	repo := &compositeOwnershipAccountRepo{
		accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"shared-openai": "gpt-5", "ambiguous": "gpt-5"}}},
			{ID: 2, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"shared-openai": "gpt-5.1"}}},
			{ID: 3, Platform: PlatformDeepseek, Credentials: map[string]any{"model_mapping": map[string]any{"ambiguous": "deepseek-v4-pro"}}},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	samePlatform, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "shared-openai")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatforms: []string{PlatformOpenAI}, Matched: true}, samePlatform)

	multiPlatform, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "ambiguous")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{TargetPlatforms: []string{PlatformDeepseek, PlatformOpenAI}, Matched: true}, multiPlatform)
}

// Scenario: 只有 OpenAI 网关族的 OAuth 声明方会被标记（API Key 上游可能兼容多协议，
// Anthropic / Antigravity 的 OAuth 在默认 Anthropic handler 中可正常转发）
func TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants(t *testing.T) {
	groupID := int64(7)
	repo := &compositeOwnershipAccountRepo{
		accounts: []Account{
			{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "gpt-6-astra"}}},
			{ID: 2, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-6-astra": "deepseek-v4-pro", "apikey-only": "deepseek-v4-pro"}}},
			{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"apikey-only": "gpt-5"}}},
			// Antigravity 账号创建时会把默认映射（含 claude-*）写入 model_mapping。
			{ID: 4, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"}}},
			{ID: 5, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Credentials: map[string]any{"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"}}},
		},
	}
	svc := &GatewayService{accountRepo: repo}

	withOAuth, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{Ambiguous: true, OpenAIGatewayOAuthClaimed: true}, withOAuth)

	apiKeyOnly, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "apikey-only")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{Ambiguous: true}, apiKeyOnly)

	anthropicFamily, err := svc.resolveCompositeModelOwnership(context.Background(), groupID, "claude-opus-4-8")
	require.NoError(t, err)
	require.Equal(t, CompositeModelOwnership{Ambiguous: true}, anthropicFamily)
}

func TestNewGatewayServiceWiresCompositeModelOwnershipResolver(t *testing.T) {
	groupID := int64(7)
	repo := &compositeOwnershipAccountRepo{
		accounts: []Account{{
			ID:          1,
			Platform:    PlatformDeepseek,
			Credentials: map[string]any{"model_mapping": map[string]any{"reasoning-alias": "deepseek-v4-pro"}},
		}},
	}
	resolver := NewCompositeRouteResolver(nil)
	svc := NewGatewayService(
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		resolver,
		nil,
		nil,
	)
	require.Same(t, resolver, svc.compositeResolver)

	decision, err := resolver.Resolve(context.Background(), groupID, "reasoning-alias", CompositeRouteEndpointResponses)
	require.NoError(t, err)
	require.True(t, decision.Matched)
	require.Equal(t, CompositeRouteSourceAccount, decision.Source)
	require.Equal(t, PlatformDeepseek, decision.TargetPlatform)
}

func TestDetectModelPlatform(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		platform string
		ok       bool
	}{
		{name: "claude", model: "claude-sonnet-4-5", platform: PlatformAnthropic, ok: true},
		{name: "anthropic prefix", model: "anthropic/claude-opus-4-5", platform: PlatformAnthropic, ok: true},
		{name: "gpt", model: "gpt-5.1", platform: PlatformOpenAI, ok: true},
		{name: "o series", model: "o3-mini", platform: PlatformOpenAI, ok: true},
		{name: "embedding", model: "text-embedding-3-large", platform: PlatformOpenAI, ok: true},
		{name: "gemini", model: "gemini-3-pro", platform: PlatformGemini, ok: true},
		{name: "gemini models prefix", model: "models/gemini-2.5-flash", platform: PlatformGemini, ok: true},
		{name: "learnlm", model: "learnlm-2.0-flash-experimental", platform: PlatformGemini, ok: true},
		{name: "grok", model: "grok-4", platform: PlatformGrok, ok: true},
		{name: "xai prefix", model: "xai/grok-4", platform: PlatformGrok, ok: true},
		{name: "kimi", model: "kimi-k2-thinking", platform: PlatformKimi, ok: true},
		{name: "kimi code bare k3", model: "K3", platform: PlatformKimi, ok: true},
		{name: "kimi code bare k3 256k", model: "k3-256k", platform: PlatformKimi, ok: true},
		{name: "kimi code provider prefix", model: "kimi-code/k3", platform: PlatformKimi, ok: true},
		{name: "moonshot prefix", model: "moonshot/moonshot-v1-32k", platform: PlatformKimi, ok: true},
		{name: "zhipu", model: "glm-5.2", platform: PlatformZhipu, ok: true},
		{name: "deepseek", model: "deepseek-v4-pro", platform: PlatformDeepseek, ok: true},
		{name: "minimax", model: "MiniMax-M3", platform: PlatformMiniMax, ok: true},
		{name: "minimax prefix", model: "minimax/MiniMax-M2.5", platform: PlatformMiniMax, ok: true},
		{name: "abab legacy", model: "abab6.5-chat", platform: PlatformMiniMax, ok: true},
		{name: "abab7 legacy", model: "abab7-chat-preview", platform: PlatformMiniMax, ok: true},
		{name: "jev", model: "jev-latest", platform: PlatformTypeSafe, ok: true},
		{name: "typesafe prefix", model: "typesafe/jev-latest", platform: PlatformTypeSafe, ok: true},
		{name: "abab unrelated namespace", model: "abab-other", ok: false},
		{name: "unknown k3 alias", model: "k3-preview", ok: false},
		{name: "unknown", model: "llama-4-maverick", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform, ok := DetectModelPlatform(tt.model)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.platform, platform)
		})
	}
}

func TestQuotaPlatformCompositeUsesResolvedOrForceOnly(t *testing.T) {
	apiKey := &APIKey{Group: &Group{Platform: PlatformComposite}}

	require.Equal(t, "", QuotaPlatform(context.Background(), apiKey))
	require.Equal(t, PlatformGemini, QuotaPlatform(WithResolvedTargetPlatform(context.Background(), PlatformGemini), apiKey))
	require.Equal(t, PlatformAntigravity, QuotaPlatform(context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAntigravity), apiKey))

	ctx := WithResolvedTargetPlatform(context.Background(), PlatformAnthropic)
	ctx = context.WithValue(ctx, ctxkey.ForcePlatform, PlatformAntigravity)
	require.Equal(t, PlatformAntigravity, QuotaPlatform(ctx, apiKey))
}

func TestCompositeGroupSchedulerHasAllCanonicalPlatformBuckets(t *testing.T) {
	seen := make(map[string]struct{})
	for _, bucket := range schedulerCanonicalBuckets(99) {
		seen[bucket.Platform] = struct{}{}
	}
	platforms := make([]string, 0, len(seen))
	for platform := range seen {
		platforms = append(platforms, platform)
	}
	require.ElementsMatch(t,
		[]string{PlatformAnthropic, PlatformGemini, PlatformOpenAI, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformTypeSafe, PlatformCommandCode, PlatformCline, PlatformWorkbuddy, PlatformQoder, PlatformTrae},
		platforms,
	)
}

func TestCompositeConcretePlatformsIncludeCNProviders(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformTypeSafe, PlatformCommandCode, PlatformCline} {
		require.True(t, isConcreteRequestPlatform(platform))
		require.True(t, canCopyAccountsFromGroupPlatform(PlatformComposite, platform))
	}
}

// Scenario: 多平台决策在账号选定后回填实际平台为单值 ctx
func TestContextWithSelectedAccountPlatformBackfillsActualPlatform(t *testing.T) {
	multi := WithResolvedTargetPlatforms(context.Background(), []string{PlatformQoder, PlatformWorkbuddy})
	apiKey := &APIKey{Group: &Group{Platform: PlatformComposite}}

	// 回填前：QuotaPlatform 为空（composite 兜底），单值不存在
	_, hasSingle := ResolvedTargetPlatformFromContext(multi)
	require.False(t, hasSingle)
	require.Empty(t, QuotaPlatform(multi, apiKey))

	// 选中集合内账号：回填该账号平台，计费配额按实际平台计量
	sel := &AccountSelectionResult{Account: &Account{ID: 1, Platform: PlatformWorkbuddy}}
	backfilled := ContextWithSelectedAccountPlatform(multi, sel)
	platform, ok := ResolvedTargetPlatformFromContext(backfilled)
	require.True(t, ok)
	require.Equal(t, PlatformWorkbuddy, platform)
	require.Equal(t, PlatformWorkbuddy, QuotaPlatform(backfilled, apiKey))

	// 选中集合外账号（调度异常兜底）：不回填假归属
	outOfSet := &AccountSelectionResult{Account: &Account{ID: 9, Platform: PlatformAnthropic}}
	unchanged := ContextWithSelectedAccountPlatform(multi, outOfSet)
	_, stillSingle := ResolvedTargetPlatformFromContext(unchanged)
	require.False(t, stillSingle)

	// 非多平台路径（无集合 key）：不做任何事
	plain := context.Background()
	require.Equal(t, plain, ContextWithSelectedAccountPlatform(plain, sel))
}
