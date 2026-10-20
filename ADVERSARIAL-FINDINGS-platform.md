# Adversarial Findings — Platform Adapters & Protocol Conversion

Scope: backend multi-platform adapters / protocol conversion. Static review + unit test run (`go test -tags=unit`).

## Failing tests observed
- `TestResolveGeminiThinkingVariant` (2 subtests): `injected_default_variants_are_usable`, `persisted_default_bare_identity_passthrough_still_resolves_when_variants_exist`
- `TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants`
- `TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership`
- `TestConfiguredUpstreamModelsForCapabilitySyncSkipsInjectedAntigravityPassthroughs`

## Findings

1. Antigravity Gemini thinking variant resolution drops requested level for injected/persisted defaults
   - File: `backend/internal/service/antigravity_gemini_thinking_variant.go` (`resolveGeminiThinkingVariant`)
   - Symptom: requesting `thinkingBudget=1000` (low) for models whose variants are injected defaults or persisted bare-identity passthrough resolves to `-high` instead of `-low`.
   - Impact: thinking budget/level not honored; upstream variant mismatch, potential 404 or wrong reasoning depth.

2. Composite model ownership never marks `Ambiguous`
   - File: `backend/internal/service/gateway_service.go` (`resolveCompositeModelOwnership`)
   - The loop only populates `platformSet` and `OpenAIGatewayOAuthClaimed`; it never sets `ownership.Ambiguous`.
   - Impact: multi-platform ownership is always treated as a schedulable collection, so OpenAI-gateway OAuth claimants are not fail-closed as intended.

3. Cross-family ownership reason mismatch
   - File: `backend/internal/service/composite_route_resolver.go` (`Resolve`)
   - Test expects generic `model is exposed by multiple provider platforms` for cross-family ownership, but code emits `... across gateway families; configure an explicit composite route...`.
   - Impact: callers/tests expecting stable reason string mismatch; possible client-facing message drift.

4. SSE usage extraction ignores zero values
   - File: `backend/internal/service/antigravity_gateway_upstream.go` (`extractSSEUsage`)
   - Conditions `int(v) > 0` skip legitimate zero token counts (e.g. cache tokens 0, output_tokens 0 in intermediate frames).
   - Impact: usage under-reporting or stale accumulation in streaming passthrough.

5. Claude SSE event type not reset between events
   - File: `backend/internal/service/antigravity_gateway_compat_stream.go` (`consumeClaudeEvents`)
   - `eventType` persists across events when a later `data:` line has no preceding `event:` line; multi-event chunks can be mislabeled.
   - Impact: stream parsing edge cases, wrong event dispatch.

6. `extractOpenAISSEDataLine`/accumulator assumes `data: ` with space
   - File: `backend/internal/service/openai_sse_data.go`
   - Lines without the exact `data: ` prefix are not recognized; SSE variants using `data:` without space may be dropped.
   - Impact: missed payloads on non-standard upstreams.

7. Workbuddy aggregator does not initialize `maxChoice` for negative/absent choices
   - File: `backend/internal/service/openai_gateway_workbuddy.go` (`workbuddyChunkAggregator.Finish`)
   - Loop `for i := 0; i <= a.maxChoice`; if choices only at index >0, index 0 slot skipped (fine), but if `maxChoice` remains 0 with no choices, fallback empty choice added. Tool-call ordering uses `maxToolCall` with `>` not `>=`, can drop index 0 updates in edge cases.
   - Impact: tool call aggregation edge-case loss.

8. Qoder chat URL only uses first candidate
   - File: `backend/internal/service/openai_gateway_qoder.go` (`qoderChatURL`)
   - Uses `GatewayCandidates(...)[0]` without fallback retry on transient host failure despite comments claiming host retry.
   - Impact: single host failure not retried.

9. Gemini compat strips function call/response IDs
   - File: `backend/internal/service/gemini_messages_compat_service.go` (`stripGeminiFunctionIDs`)
   - Deletes `id` from `functionCall`/`functionResponse`, relying solely on positional correlation.
   - Impact: tool call ID mismatch if upstream reorders or requires IDs.

10. Gemini thinking level budget thresholds
    - File: `backend/internal/service/antigravity_gemini_thinking_variant.go` (`geminiThinkingLevelFromBody`)
    - `budget <= 1024` maps to low including 0 (thinking disabled), but no variant for "no thinking"; upstream may reject or default incorrectly.
    - Impact: thinking disabled requests still route to `-low` variant.

11. Composite ownership cache key ignores ambiguous flag changes? (minor)
    - File: `backend/internal/service/gateway_service.go`
    - Cached ownership may stale across config changes; invalidation only on model list cache.
    - Impact: stale ambiguous state.

## Recommendation
Prioritize fixes #1, #2, #4; add regression tests for SSE multi-event and tool-call ID correlation. No code changes made per task.
