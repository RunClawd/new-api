# BaseGate Phase 10: Circuit Breaker, Pre-authorization & Tenant Management

## Background

Phase 1-9 delivered a fully functional capability gateway with 3 verified native adapters (LLM/Video/Sandbox), admin dashboard, and production-hardened core engine. The remaining gaps that block real multi-tenant deployment are:

| Gap | Impact | Current State |
|-----|--------|---------------|
| No circuit breaker | Failing adapters get retried on every request, wasting latency | Stateless fallback loop, no failure memory |
| No pre-authorization | Users can exhaust quota mid-request, billing after the fact | `FinalizeBilling` only runs post-completion |
| No tenant management API | Orgs/Projects cannot be managed programmatically | Fields exist on all tables but no CRUD |
| Stale P1 items | Poll backoff listed but already done in Phase 6 | Doc cleanup needed |

**Strategy:** Three independent workstreams that can be implemented in any order. Circuit breaker is the highest-value item — it directly improves reliability for all existing adapters.

---

## Open Questions

> [!IMPORTANT]
> 1. **Circuit breaker scope**: Per-adapter or per-adapter-per-capability? A single OpenAI adapter might fail for `bg.llm.chat.pro` (rate limited) but work fine for `bg.llm.chat.fast`. Recommendation: per-adapter (simpler), since adapter-level failures (auth, network) are more common than model-level ones.
> 2. **Pre-auth quota source**: Deduct from the existing user quota system (`model/user.go` Quota field) or from a new BaseGate-specific balance? Recommendation: bridge to existing quota system for consistency.
> 3. **Tenant management**: Full Organization/Project CRUD, or start with read-only admin view + project-scoped API key creation? Recommendation: start with admin CRUD for projects only; orgs map to users in the current identity model.

---

## Step 1: Circuit Breaker (P2, 1-2d)

### Design

Three-state circuit breaker per adapter, stored in-memory alongside the adapter registry:

```
CLOSED (normal) ──[failures >= threshold]──> OPEN (reject all)
                                                │
                                    [cooldown elapsed]
                                                │
                                                v
                                          HALF-OPEN (probe)
                                                │
                                    ┌───────────┴──────────┐
                               [probe succeeds]       [probe fails]
                                    │                      │
                                    v                      v
                                  CLOSED                 OPEN (reset cooldown)
```

**Configuration (with defaults):**

| Parameter | Default | Description |
|-----------|---------|-------------|
| `FailureThreshold` | 5 | Consecutive failures to trip open |
| `CooldownSec` | 60 | Seconds in OPEN before allowing a probe |
| `HalfOpenMaxProbes` | 1 | Concurrent probes allowed in HALF-OPEN |

### Files

#### [NEW] `relay/basegate/circuit_breaker.go`
- `AdapterCircuitBreaker` struct: `state`, `failureCount`, `lastFailureAt`, `lastProbeAt`
- `CircuitBreakerRegistry`: global `map[string]*AdapterCircuitBreaker` (keyed by adapter name), `sync.RWMutex`
- `RecordSuccess(adapterName)`: reset failure count, transition HALF-OPEN → CLOSED
- `RecordFailure(adapterName)`: increment count, trip OPEN if threshold reached
- `CanAttempt(adapterName) bool`: CLOSED=yes, OPEN=check cooldown→maybe HALF-OPEN, HALF-OPEN=limited probes
- `GetCircuitState(adapterName) string`: for admin API / dashboard display

#### [MODIFY] `service/bg_orchestrator.go`
- In the fallback loop (both `DispatchSync` and `DispatchAsync`), before `adapter.Invoke()`:
  ```go
  if !basegate.CanAttempt(adapter.Name()) {
      common.SysLog("circuit open for " + adapter.Name())
      continue // skip to next adapter
  }
  ```
- After successful invoke: `basegate.RecordSuccess(adapter.Name())`
- After failed invoke (both `invokeErr != nil` and `provider_unavailable`): `basegate.RecordFailure(adapter.Name())`

#### [MODIFY] `service/bg_streaming.go`
- Same circuit breaker integration for the streaming dispatch path.

#### [MODIFY] `controller/bg_admin.go`
- `AdminGetBgUsageStats` enhanced with per-adapter circuit breaker state:
  ```json
  {
    "adapters": [
      {"name": "openai_native_ch1", "state": "closed", "failure_count": 0},
      {"name": "kling_native_ch2", "state": "open", "failure_count": 7, "cooldown_remaining_sec": 45}
    ]
  }
  ```

#### [NEW] `relay/basegate/circuit_breaker_test.go`
- Test state transitions: CLOSED → OPEN after N failures
- Test OPEN → HALF-OPEN after cooldown
- Test HALF-OPEN → CLOSED on success
- Test HALF-OPEN → OPEN on probe failure
- Test concurrent access safety

---

## Step 2: Pre-authorization (P2, 2d)

### Design

Before dispatching a request, estimate the cost and verify the user has sufficient quota. If not, reject with `402 Payment Required` before touching any provider.

```
Request arrives
  │
  ├─ LookupPricing(model) → PricingSnapshot
  ├─ EstimateCost(snapshot, input) → estimated_amount
  ├─ TryReserveQuota(org_id, estimated_amount) → ok / insufficient
  │     │
  │     ├─ insufficient → 402 + "insufficient_quota"
  │     └─ ok → proceed with dispatch
  │
  ├─ ... adapter invocation ...
  │
  └─ FinalizeBilling(actual_usage)
       ├─ actual ≤ estimated → refund difference
       └─ actual > estimated → charge difference (best-effort)
```

### Files

#### [NEW] `service/bg_preauth.go`
- `EstimateCost(pricing *PricingSnapshot, input interface{}) float64`:
  - LLM: estimate from input token count (rough: `len(inputStr) / 4`)
  - Video/Sandbox: use minimum billable unit (1 second / 1 minute)
  - Fallback: `pricing.UnitPrice * 1.0` (1 unit minimum)
- `TryReserveQuota(orgID int, amount float64) (reservationID string, err error)`:
  - Bridge to existing `model.DecreaseUserQuota()` / `model.GetUserQuota()`
  - Write a `bg_ledger_entries` row with `entry_type = "hold"` and `status = "pending"`
  - Return error if quota insufficient
- `SettleReservation(reservationID string, actualAmount float64)`:
  - If actual < estimated: credit the difference back
  - If actual > estimated: debit the difference (best-effort, log warning)
  - Update hold entry status to "settled"

#### [MODIFY] `service/bg_orchestrator.go`
- `DispatchSync` / `DispatchAsync`: after pricing snapshot, before adapter loop:
  ```go
  reservationID, err := TryReserveQuota(req.OrgID, estimatedCost)
  if err != nil {
      return nil, ErrInsufficientQuota
  }
  ```
- After `ApplyProviderEvent` terminal: `SettleReservation(reservationID, actualCost)`

#### [MODIFY] `controller/bg_responses.go`
- Detect `ErrInsufficientQuota` → return `402 Payment Required`

#### [MODIFY] `model/bg_billing.go`
- `BgLedgerEntry`: add `"hold"` as valid `EntryType` alongside `"debit"` / `"credit"`
- Add `ReservationID` field for linking hold → settlement

#### [NEW] `service/bg_preauth_test.go`
- Test estimation for different capability types
- Test reserve → settle with refund
- Test reserve → insufficient quota rejection

---

## Step 3: Tenant Project Management (P3 start, 1-2d)

### Design

The identity model currently maps `OrgID` = `UserID` (single-user-per-org). Projects exist as a scoping concept on all tables (`project_id` field) but have no management API. This step adds:

1. A `bg_projects` table for CRUD
2. Admin API for project management
3. Project-scoped API key linking

### Files

#### [NEW] `model/bg_project.go`
- `BgProject` struct: `ID`, `ProjectID` (string, unique), `OrgID`, `Name`, `Description`, `Status`, `CreatedAt`
- CRUD functions: `CreateBgProject`, `GetBgProject`, `ListBgProjectsByOrgID`, `UpdateBgProject`, `DeleteBgProject`

#### [MODIFY] `model/main.go`
- Add `BgProject` to AutoMigrate list

#### [NEW] `controller/bg_project.go`
- `AdminListBgProjects` — `GET /api/bg/projects` (admin, optionally filtered by `org_id`)
- `AdminCreateBgProject` — `POST /api/bg/projects`
- `AdminUpdateBgProject` — `PUT /api/bg/projects/:id`
- `AdminDeleteBgProject` — `DELETE /api/bg/projects/:id`

#### [MODIFY] `router/api-router.go`
- Add project routes under the existing `/api/bg/` admin group

#### Frontend

#### [NEW] `web/src/pages/BgProjects/index.jsx`
- Project list table with create/edit/delete modals
- Columns: Project ID, Name, Org, Status, Created At, Actions

#### [MODIFY] `web/src/App.jsx` + `SiderBar.jsx` + `render.jsx`
- Add `/console/bg-projects` route and sidebar entry

#### [MODIFY] `web/src/i18n/locales/zh-CN.json` + `en.json`
- Add project management i18n keys

---

## Step 4: Cleanup & Doc Updates (0.5d)

#### [MODIFY] `docs/BASEGATE.md`
- Remove stale "Poll backoff strategy" from P1 (already done in Phase 6)
- Move completed P2 items to Completed section
- Update Completion Summary percentages
- Add Phase 10 to Development Timeline

#### [NEW] `controller/bg_e2e_session_test.go` (P0 coverage)
- Test session lifecycle via controller: POST /sessions → GET status → POST action → POST close
- Uses `DummySandboxAdapter` mock for deterministic testing

---

## Verification Plan

### Automated Tests
```bash
# Circuit breaker state machine
go test ./relay/basegate/... -run "TestCircuitBreaker" -v -count=1

# Pre-auth reservation lifecycle
go test ./service/... -run "TestPreAuth" -v -count=1

# Session E2E
go test ./controller/... -run "TestE2E_Session" -v -count=1

# Full regression
go test ./model/... ./dto/... ./service/... ./controller/... ./relay/basegate/... -count=1
```

### Manual Verification
- Trigger circuit breaker: configure adapter with invalid key → send 5+ requests → verify fallback skips it → wait cooldown → verify probe attempt
- Pre-auth rejection: set user quota to 0 → POST /v1/bg/responses → verify 402
- Project CRUD: create/list/update/delete via admin dashboard

---

## Execution Order

| Day | Content | Dependencies |
|-----|---------|--------------|
| Day 1 | Step 1: Circuit Breaker (core + orchestrator integration + tests) | None |
| Day 2 | Step 2: Pre-authorization (estimation + reservation + settlement) | Pricing snapshot (Phase 7) |
| Day 3 | Step 3: Project Management (model + API + frontend page) | Admin APIs (Phase 8) |
| Day 3+ | Step 4: Cleanup + session E2E test | Steps 1-3 complete |

Steps 1 and 2 are independent and can be parallelized. Step 3 depends only on the existing admin API pattern.
