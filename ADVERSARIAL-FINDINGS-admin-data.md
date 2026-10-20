# Adversarial Findings - Admin Data (backend)

Scope: `backend/internal/handler/admin/*`, `backend/internal/service/*admin*`, `backend/internal/repository/*`, `backend/ent/schema/*`, `backend/migrations/*`, `backend/internal/setup/*`, `backend/internal/model/*`.

## Findings

1. **Generated Ent `User` serializes `password_hash`**
   - Location: `backend/ent/user.go` (`PasswordHash string `json:"password_hash,omitempty"``)
   - Issue: The generated entity includes a JSON tag for the password hash. Any endpoint or debug path that serializes `*dbent.User` directly will leak password hashes. DTO mapping is used in most admin handlers, but the generated type remains unsafe.
   - Impact: Credential hash disclosure if any code path returns the raw entity.
   - Recommendation: Remove/blank the JSON tag in generated code or ensure no handler serializes raw `dbent.User`.

2. **Last-admin protection has a TOCTOU race**
   - Location: `backend/internal/service/admin_user.go` (`ensureNotLastAdmin`)
   - Issue: Admin count is read, then the demotion write happens later without a DB-level constraint or serializable transaction. Concurrent requests can both pass the `Total <= 1` check and demote all admins.
   - Impact: Lockout / zero-admin state.
   - Recommendation: Use a transaction with row-level locking or a DB constraint on admin count.

3. **`DeleteAccount` is not atomic across shadow cascade**
   - Location: `backend/internal/service/admin_account.go` (`DeleteAccount`)
   - Issue: Shadow accounts are deleted one-by-one, then the parent is deleted, without a shared transaction. A failure midway leaves partially deleted account graph.
   - Impact: Orphaned shadow accounts / inconsistent scheduling state.
   - Recommendation: Wrap shadow + parent deletion in one transaction.

4. **`ClearAccountError` performs multiple writes without transaction**
   - Location: `backend/internal/service/admin_account.go` (`ClearAccountError`)
   - Issue: `ClearError`, `ClearRateLimit`, `ClearAntigravityQuotaScopes`, `ClearModelRateLimits`, `ClearTempUnschedulable` are separate repository calls; partial failure leaves inconsistent account error state.
   - Impact: Stale rate-limit/quota flags after "clear error".
   - Recommendation: Execute in a single transaction.

5. **`BatchDeleteProxies` TOCTOU between in-use check and delete**
   - Location: `backend/internal/service/admin_proxy.go` (`BatchDeleteProxies`)
   - Issue: `CountAccountsByProxyID` is checked, then `Delete` runs later without locking. An account can be assigned to the proxy in between, deleting an in-use proxy.
   - Impact: Dangling account proxy references.
   - Recommendation: Delete with a conditional query (`DELETE ... WHERE NOT EXISTS accounts`) or use a transaction/lock.

6. **API key secret returned in admin/user list responses**
   - Location: `backend/internal/handler/dto/mappers.go` (`APIKeyFromService`, `Key: k.Key`)
   - Issue: Full API key material is returned by list/detail endpoints rather than masked. Admin UI and logs can expose reusable secrets.
   - Impact: Secret disclosure beyond creation time.
   - Recommendation: Mask key except on creation, return only suffix.

7. **Setup prints generated admin password to stdout**
   - Location: `backend/internal/setup/setup.go` (`fmt.Printf("Generated admin password (one-time): %s\n", ...)`)
   - Issue: The one-time admin password is written to process stdout, which is commonly captured by container/runtime logs.
   - Impact: Credential leakage via logs.
   - Recommendation: Avoid printing secret; write to a secured file or require explicit retrieval.

8. **Audit log full clear has no retention/safety guard beyond TOTP**
   - Location: `backend/handler/admin/audit_log_handler.go` (`Clear`)
   - Issue: ClearAll removes all audit history after TOTP; no backup/export or minimum retention. An attacker with a valid admin session can erase evidence.
   - Impact: Loss of audit trail.
   - Recommendation: Require dual control / export before clear / append-only storage.

9. **Migration checksum compatibility rules weaken immutability**
   - Location: `backend/internal/repository/migrations_runner.go` (`migrationChecksumCompatibilityRules`, `isMigrationChecksumCompatible`)
   - Issue: A whitelist allows certain applied migrations to be replaced if both DB and file checksums are in a known set. This relaxes the immutability guarantee for those files.
   - Impact: Tampered migration could pass validation for allowlisted files.
   - Recommendation: Remove compatibility exceptions once production DBs are aligned; keep strict checksums.

10. **Scheduled test handler returns raw service structs**
    - Location: `backend/internal/handler/admin/scheduled_test_handler.go` (`c.JSON(http.StatusOK, plans/created/updated)`)
    - Issue: Raw service model is serialized directly, bypassing DTO mapping and potentially exposing internal fields or inconsistent shapes.
    - Impact: Schema drift / accidental field exposure.
    - Recommendation: Map to DTO before response.

11. **Redeem/promo MaxUses=0 means unlimited**
    - Location: `backend/internal/handler/admin/promo_handler.go` (`MaxUses int ... min=0`)
    - Issue: Zero is interpreted as unlimited usage, easy to create unbounded promo codes by mistake.
    - Impact: Unintended unlimited redemption.
    - Recommendation: Use explicit unlimited flag or require positive max uses.

12. **User role demotion self-check only in handler, not service**
    - Location: `backend/internal/handler/admin/user_handler.go` (`Update`, self-demotion check) and `backend/internal/service/admin_user.go`
    - Issue: The "cannot demote yourself" guard lives only in HTTP handler. Any other caller of `UpdateUser` (tests, future RPC) can bypass it.
    - Impact: Admin lockout via non-HTTP path.
    - Recommendation: Move self-demotion protection into service layer with actor ID context.
