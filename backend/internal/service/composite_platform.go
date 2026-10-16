package service

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// WithResolvedTargetPlatform stores the concrete provider chosen for a request
// made through a composite group.
func WithResolvedTargetPlatform(ctx context.Context, platform string) context.Context {
	platform = strings.TrimSpace(platform)
	if ctx == nil || platform == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.ResolvedTargetPlatform, platform)
}

// ResolvedTargetPlatformFromContext returns the concrete provider chosen for
// the current request, if one was resolved.
func ResolvedTargetPlatformFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	platform, ok := ctx.Value(ctxkey.ResolvedTargetPlatform).(string)
	platform = strings.TrimSpace(platform)
	if !ok || platform == "" {
		return "", false
	}
	return platform, true
}

// WithResolvedTargetPlatforms stores the same-gateway-family provider set
// resolved for a composite multi-platform decision. The single-platform key
// stays unset until an account is selected.
func WithResolvedTargetPlatforms(ctx context.Context, platforms []string) context.Context {
	normalized := NormalizeCompositePlatformSet(platforms)
	if ctx == nil || len(normalized) < 2 {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.ResolvedTargetPlatforms, normalized)
}

// ResolvedTargetPlatformsFromContext returns the multi-platform provider set
// for the current request, if a composite decision resolved to more than one
// platform.
func ResolvedTargetPlatformsFromContext(ctx context.Context) ([]string, bool) {
	if ctx == nil {
		return nil, false
	}
	platforms, ok := ctx.Value(ctxkey.ResolvedTargetPlatforms).([]string)
	if !ok || len(platforms) < 2 {
		return nil, false
	}
	return platforms, true
}

// ContextWithSelectedAccountPlatform 在复合分组多平台决策下，把选号结果中
// 账号的实际平台回填为单值 ResolvedTargetPlatform，使计费配额（QuotaPlatform）、
// 渠道费率（channelLookupPlatform）、ops 平台归因等单值消费者无需感知集合。
// 仅当 ctx 处于「多平台集合且无单值」状态时写入；failover 换号后每次重选重调，
// 天然 per-attempt 正确。单平台路径（无集合 key）不做任何事。
func ContextWithSelectedAccountPlatform(ctx context.Context, sel *AccountSelectionResult) context.Context {
	if ctx == nil || sel == nil || sel.Account == nil {
		return ctx
	}
	if _, hasSingle := ResolvedTargetPlatformFromContext(ctx); hasSingle {
		return ctx
	}
	platforms, multi := ResolvedTargetPlatformsFromContext(ctx)
	if !multi {
		return ctx
	}
	if !slices.Contains(platforms, sel.Account.Platform) {
		// 调度层异常兜底：选中账号不在候选集合内时不回填假归属。
		return ctx
	}
	return WithResolvedTargetPlatform(ctx, sel.Account.Platform)
}

// NormalizeCompositePlatformSet trims, deduplicates (case-insensitive) and
// sorts a platform set for deterministic storage and cache/ctx reuse.
func NormalizeCompositePlatformSet(platforms []string) []string {
	if len(platforms) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(platforms))
	normalized := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			continue
		}
		lower := strings.ToLower(platform)
		if _, dup := seen[lower]; dup {
			continue
		}
		seen[lower] = struct{}{}
		normalized = append(normalized, platform)
	}
	sort.Strings(normalized)
	return normalized
}

// CompositePlatformsShareGatewayFamily reports whether every platform in the
// set is served by the same gateway family (domain.UsesOpenAIGateway binary
// split). Ownership that spans families stays fail-closed because a single
// request can only enter one gateway handler chain.
func CompositePlatformsShareGatewayFamily(platforms []string) bool {
	if len(platforms) <= 1 {
		return true
	}
	first := domain.UsesOpenAIGateway(platforms[0])
	for _, platform := range platforms[1:] {
		if domain.UsesOpenAIGateway(platform) != first {
			return false
		}
	}
	return true
}

func WithCompositeRouteDecision(ctx context.Context, decision CompositeRouteDecision) context.Context {
	if ctx == nil || !decision.Matched {
		return ctx
	}
	// 多平台决策：写集合、不写单值（避免下游读到假平台）；账号选定后由
	// ContextWithSelectedAccountPlatform 回填实际平台为单值。
	if len(decision.TargetPlatforms) > 1 {
		ctx = WithResolvedTargetPlatforms(ctx, decision.TargetPlatforms)
	} else {
		ctx = WithResolvedTargetPlatform(ctx, decision.TargetPlatform)
	}
	if model := strings.TrimSpace(decision.UpstreamModel); model != "" {
		ctx = context.WithValue(ctx, ctxkey.ResolvedUpstreamModel, model)
	}
	if model := strings.TrimSpace(decision.PublicModel); model != "" {
		ctx = context.WithValue(ctx, ctxkey.RequestedPublicModel, model)
	}
	if source := strings.TrimSpace(decision.Source); source != "" {
		ctx = context.WithValue(ctx, ctxkey.CompositeRouteSource, source)
	}
	return ctx
}

func ResolvedUpstreamModelFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	model, ok := ctx.Value(ctxkey.ResolvedUpstreamModel).(string)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return "", false
	}
	return model, true
}

func RequestedPublicModelFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	model, ok := ctx.Value(ctxkey.RequestedPublicModel).(string)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return "", false
	}
	return model, true
}

func CompositeRouteSourceFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	source, ok := ctx.Value(ctxkey.CompositeRouteSource).(string)
	source = strings.TrimSpace(source)
	if !ok || source == "" {
		return "", false
	}
	return source, true
}

// DetectModelPlatform maps common public model IDs to the concrete provider
// platform used by sub2api. It intentionally returns false for ambiguous model
// names so composite groups fail closed instead of guessing.
func DetectModelPlatform(model string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return "", false
	}

	normalized = strings.TrimPrefix(normalized, "models/")
	if slash := strings.IndexByte(normalized, '/'); slash > 0 {
		provider := strings.TrimSpace(normalized[:slash])
		rest := strings.TrimSpace(normalized[slash+1:])
		switch provider {
		case "anthropic", "claude":
			return PlatformAnthropic, true
		case "openai", "chatgpt":
			return PlatformOpenAI, true
		case "google", "google-ai-studio", "gemini":
			return PlatformGemini, true
		case "xai", "x-ai", "grok":
			return PlatformGrok, true
		case "kimi", "moonshot":
			return PlatformKimi, true
		case "zhipu", "glm", "bigmodel":
			return PlatformZhipu, true
		case "deepseek":
			return PlatformDeepseek, true
		case "minimax":
			return PlatformMiniMax, true
		case "typesafe", "jev":
			return PlatformTypeSafe, true
		}
		if rest != "" {
			normalized = strings.TrimPrefix(rest, "models/")
		}
	}

	switch {
	case strings.HasPrefix(normalized, "anthropic.claude-"),
		strings.HasPrefix(normalized, "claude-"):
		return PlatformAnthropic, true
	case strings.HasPrefix(normalized, "gpt-"),
		strings.HasPrefix(normalized, "chatgpt-"),
		strings.HasPrefix(normalized, "codex-"),
		strings.HasPrefix(normalized, "text-embedding-"),
		strings.HasPrefix(normalized, "text-moderation-"),
		strings.HasPrefix(normalized, "omni-moderation-"),
		strings.HasPrefix(normalized, "dall-e-"),
		strings.HasPrefix(normalized, "gpt-image-"),
		strings.HasPrefix(normalized, "tts-"),
		strings.HasPrefix(normalized, "whisper-"),
		hasOpenAISeriesPrefix(normalized):
		return PlatformOpenAI, true
	case strings.HasPrefix(normalized, "gemini-"),
		strings.HasPrefix(normalized, "learnlm-"):
		return PlatformGemini, true
	case normalized == "grok" || strings.HasPrefix(normalized, "grok-"):
		return PlatformGrok, true
	case normalized == "k3",
		normalized == "k3-256k",
		strings.HasPrefix(normalized, "kimi-"),
		strings.HasPrefix(normalized, "moonshot-"):
		return PlatformKimi, true
	case strings.HasPrefix(normalized, "glm-"):
		return PlatformZhipu, true
	case strings.HasPrefix(normalized, "deepseek-"):
		return PlatformDeepseek, true
	case strings.HasPrefix(normalized, "minimax-"),
		strings.HasPrefix(normalized, "abab5"),
		strings.HasPrefix(normalized, "abab6"),
		strings.HasPrefix(normalized, "abab7"):
		return PlatformMiniMax, true
	case normalized == "jev-latest" || strings.HasPrefix(normalized, "jev-"):
		return PlatformTypeSafe, true
	default:
		return "", false
	}
}

func hasOpenAISeriesPrefix(model string) bool {
	for _, prefix := range []string{"o1", "o3", "o4", "o5"} {
		if model == prefix || strings.HasPrefix(model, prefix+"-") {
			return true
		}
	}
	return false
}

func (s *GatewayService) resolveCompositeRouteDecision(ctx context.Context, group *Group, requestedModel, endpoint string) (CompositeRouteDecision, bool, error) {
	if group == nil || group.Platform != PlatformComposite {
		return CompositeRouteDecision{}, false, nil
	}
	if platform, ok := ResolvedTargetPlatformFromContext(ctx); ok {
		upstreamModel := requestedModel
		if resolvedModel, modelOK := ResolvedUpstreamModelFromContext(ctx); modelOK {
			upstreamModel = resolvedModel
		}
		source := CompositeRouteSourceDetector
		if resolvedSource, sourceOK := CompositeRouteSourceFromContext(ctx); sourceOK {
			source = resolvedSource
		}
		return CompositeRouteDecision{
			Matched:        true,
			Source:         source,
			GroupID:        group.ID,
			PublicModel:    requestedModel,
			TargetPlatform: platform,
			UpstreamModel:  upstreamModel,
			Endpoint:       normalizeCompositeRouteEndpoint(endpoint),
		}, true, nil
	}
	if platforms, ok := ResolvedTargetPlatformsFromContext(ctx); ok {
		upstreamModel := requestedModel
		if resolvedModel, modelOK := ResolvedUpstreamModelFromContext(ctx); modelOK {
			upstreamModel = resolvedModel
		}
		source := CompositeRouteSourceDetector
		if resolvedSource, sourceOK := CompositeRouteSourceFromContext(ctx); sourceOK {
			source = resolvedSource
		}
		return CompositeRouteDecision{
			Matched:         true,
			Source:          source,
			GroupID:         group.ID,
			PublicModel:     requestedModel,
			TargetPlatforms: platforms,
			UpstreamModel:   upstreamModel,
			Endpoint:        normalizeCompositeRouteEndpoint(endpoint),
		}, true, nil
	}
	decision, err := s.compositeResolver.Resolve(ctx, group.ID, requestedModel, endpoint)
	if err != nil {
		return decision, false, err
	}
	return decision, decision.Matched, nil
}

func isConcreteRequestPlatform(platform string) bool {
	return domain.IsConcretePlatform(platform)
}
