# Adversarial Findings - Frontend (Vue 3 + TS + Vite)

Scope: `frontend/src` (views, components, stores, api, router, composables, utils, i18n, types, features).

## Findings

1. **Admin-controlled home page HTML rendered without sanitization**
   - Location: `frontend/src/views/HomeView.vue` (`<div v-else v-html="homeContent"></div>`)
   - Issue: `homeContent` comes from public settings (`home_content`) and is injected via `v-html` with no DOMPurify pass, unlike other markdown views. The comment explicitly accepts the XSS risk.
   - Impact: Stored XSS if an admin account is compromised or a malicious setting is served.
   - Recommendation: Sanitize with `DOMPurify.sanitize(homeContent)` before rendering, or restrict to markdown + sanitize.

2. **Auth access token leaked into iframe query string**
   - Location: `frontend/src/views/user/CustomPageView.vue` (`embeddedUrl`) -> `frontend/src/utils/embedded-url.ts` (`buildEmbeddedUrl`)
   - Issue: `authStore.token` is placed as a `token` query parameter on the embedded iframe URL. Tokens in URLs are captured by browser history, referrer headers, server access logs, and analytics.
   - Impact: Credential disclosure beyond the intended session.
   - Recommendation: Avoid passing bearer tokens in URLs; use same-origin cookies or postMessage-based handoff.

3. **"Open in new tab" button href not validated/sanitized**
   - Location: `frontend/src/views/user/CustomPageView.vue` (`:href="embeddedUrl"`, `openButton`)
   - Issue: `buildEmbeddedUrl` returns the raw `baseUrl` unchanged when `new URL()` throws. A non-HTTP(S) value (e.g. `javascript:`) can therefore reach the anchor `href`, and `isValidUrl` only gates the iframe, not the button.
   - Impact: Clicking "open in new tab" can execute attacker-controlled JavaScript.
   - Recommendation: Validate/sanitize the URL before binding to `href`; reject non-http(s) schemes.

4. **Ring animation timers/RAFs not fully cancelled on unmount**
   - Location: `frontend/src/views/KeyUsageView.vue` (`triggerRingAnimation`, `cancelRingAnimation`)
   - Issue: `cancelRingAnimation()` only cancels the currently stored frame/timer. Nested `requestAnimationFrame`/`setTimeout` callbacks scheduled inside an in-flight frame can still fire after teardown, as observed by the unhandled `requestAnimationFrame is not defined` errors in tests.
   - Impact: Console errors, leaked timers, potential state updates after unmount.
   - Recommendation: Track all scheduled handles and cancel them recursively; guard callbacks with a mounted flag.

5. **Auth tokens stored in `localStorage` (XSS-exfiltratable)**
   - Location: `frontend/src/stores/auth.ts` (`AUTH_TOKEN_KEY`, `REFRESH_TOKEN_KEY`, `localStorage.setItem`)
   - Issue: Access and refresh tokens are persisted in `localStorage`, which is readable by any script executing on the origin.
   - Impact: Any XSS vulnerability can exfiltrate long-lived session credentials.
   - Recommendation: Prefer httpOnly, Secure, SameSite cookies for token storage; at minimum avoid storing refresh tokens client-side.

6. **Backend-mode route allowlist uses unsafe prefix matching**
   - Location: `frontend/src/router/index.ts` (`isBackendModePublicRouteAllowed`)
   - Issue: The check uses `path.startsWith(allowedPath)` without a path boundary, so `/loginX`, `/setupfoo`, etc. are treated as allowed.
   - Impact: Backend-mode restrictions can be bypassed for crafted routes.
   - Recommendation: Match exact paths or enforce a trailing slash/segment boundary.

7. **Risk-control guard fails open when settings cannot load**
   - Location: `frontend/src/router/index.ts` (risk-control block, `fetchPublicSettings` catch)
   - Issue: The guard only blocks when settings are loaded and `risk_control_enabled === false`. If the fetch fails, enforcement is skipped.
   - Impact: Restricted risk-control pages may be reachable during settings outages.
   - Recommendation: Fail closed (block) on load failure or retry before allowing navigation.

8. **SVG upload emits raw file content; sanitization only at render**
   - Location: `frontend/src/components/common/ImageUpload.vue` (`handleUpload`, `sanitizedValue`)
   - Issue: In SVG mode the raw file text is emitted to the parent via `update:modelValue`; sanitization (`sanitizeSvg`) happens only in this component's preview. Any consumer that renders the stored value without sanitizing reintroduces XSS.
   - Impact: Stored XSS if a consumer renders the unsanitized value.
   - Recommendation: Sanitize before emitting/storing, or store the sanitized SVG.

9. **Excessive `any` usage weakens type safety**
   - Location: across `frontend/src` (267 `: any`, 155 `as any`)
   - Issue: Widespread `any` annotations bypass compile-time checks in views, stores, and API layers.
   - Impact: Runtime type errors are not caught by `vue-tsc`; refactoring regressions slip through.
   - Recommendation: Replace `any` with precise types or `unknown` plus narrowing, starting with API boundaries.

10. **Copy-button listeners injected into markdown DOM are never removed**
    - Location: `frontend/src/views/user/CustomPageView.vue` (`injectCopyButtons`)
    - Issue: `addEventListener('click', ...)` is attached to each `<pre>`'s copy button but never detached; repeated renders can accumulate listeners.
    - Impact: Minor memory leak and duplicate clipboard actions.
    - Recommendation: Remove listeners on unmount or use event delegation.

11. **Iframe home content URL not validated beyond prefix**
    - Location: `frontend/src/views/HomeView.vue` (`isHomeContentUrl`, `:src="homeContent.trim()"`)
    - Issue: Only a `startsWith('http://')/('https://')` check is performed; the value is not parsed/validated, allowing malformed or deceptive URLs.
    - Impact: Potential phishing/redirect via admin-controlled setting.
    - Recommendation: Parse with `new URL()` and allow only http/https before binding `src`.

12. **`window.matchMedia`/theme access assumes browser globals**
    - Location: `frontend/src/views/HomeView.vue`, `frontend/src/views/KeyUsageView.vue` (direct `window`/`document` access in setup)
    - Issue: Direct global access in component setup can break SSR or non-browser test environments.
    - Impact: Fragility in non-DOM contexts.
    - Recommendation: Guard with `typeof window !== 'undefined'` where appropriate.
