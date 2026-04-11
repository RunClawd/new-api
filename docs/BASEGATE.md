# BaseGate — Unified Capability Gateway

## Overview

BaseGate is a unified capability gateway layer built into new-api. It abstracts LLM, image/video generation, browser automation, sandbox execution, and other heterogeneous AI services behind a single API, routing, billing, and governance plane.

**One Key, one endpoint, one bill — for all AI capabilities an Agent needs.**

## Core Principles

- **Everything is a Model-backed Capability** — external consumers call stable capability names (`bg.llm.chat.standard`), not vendor-specific models
- **Everything is routed through a Canonical Request into a Provider Adapter** — internal execution is unified regardless of upstream provider
- **Four-layer accounting separation** — Response (execution truth), Usage (resource truth), Billing (pricing truth), Ledger (money truth)

## Architecture

```
Client Request
  │
  ├── POST /v1/bg/responses          (sync / async / stream)
  ├── POST /v1/bg/sessions            (session-mode capabilities)
  ├── GET  /v1/bg/responses/:id       (poll async status)
  ├── POST /v1/bg/responses/:id/cancel
  ├── GET  /v1/bg/sessions/:id
  ├── POST /v1/bg/sessions/:id/action
  └── POST /v1/bg/sessions/:id/close
         │
         ▼
┌─────────────────────────────────────────────────────┐
│                   Controller Layer                   │
│  bg_responses.go  │  bg_sessions.go  │  model.go    │
└────────┬──────────┴─────────┬────────┴──────────────┘
         │                    │
         ▼                    ▼
┌─────────────────────────────────────────────────────┐
│                   Service Layer                      │
│                                                     │
│  Orchestrator         Session Manager               │
│  ├ DispatchSync       ├ CreateSession               │
│  ├ DispatchAsync      ├ ExecuteSessionAction        │
│  └ DispatchStream     ├ CloseSession                │
│                       └ (CAS lock + idempotency)    │
│                                                     │
│  State Machine        Billing Engine                │
│  ├ ApplyProviderEvent ├ FinalizeBilling (txn)       │
│  ├ Auto-advance       ├ FinalizeSessionBilling      │
│  └ CAS concurrency    └ LookupPricing              │
│                                                     │
│  Background Workers                                 │
│  ├ BgPollWorker       (async task polling)          │
│  ├ BgSessionWorker    (idle/expire enforcement)     │
│  └ BgWebhookWorker    (outbox delivery + retry)     │
│                                                     │
│  Streaming            Outbox                        │
│  └ DispatchStream     └ EnqueueWebhookEvent         │
│                                                     │
│  Audit                                              │
│  └ RecordBgAuditLog   (async, non-blocking)         │
└────────┬────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────┐
│               Adapter Layer (relay/basegate/)         │
│                                                     │
│  ProviderAdapter interface (7 methods):             │
│  ├ Name() / DescribeCapabilities() / Validate()     │
│  ├ Invoke() / Poll() / Cancel() / Stream()          │
│  └ SessionCapableAdapter extension:                 │
│    ├ CreateSession() / ExecuteAction()              │
│    └ CloseSession() / GetSessionStatus()            │
│                                                     │
│  Registry: 1:N capability → adapter mapping         │
│  ├ LookupAdapters(model)     → []ProviderAdapter    │
│  └ LookupAdapterByName(name) → ProviderAdapter      │
│                                                     │
│  Implementations:                                   │
│  ├ OpenAILLMAdapter     (native, raw HTTP)          │
│  └ LegacyTaskAdaptorWrapper (bridge to existing)    │
└────────┬────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────┐
│                  Data Layer (model/)                  │
│                                                     │
│  Core Tables (10):                                  │
│  ├ bg_responses           (execution truth)         │
│  ├ bg_response_attempts   (provider-level tracking) │
│  ├ bg_sessions            (stateful capabilities)   │
│  ├ bg_session_actions     (session operation log)   │
│  ├ bg_usage_records       (resource consumption)    │
│  ├ bg_billing_records     (pricing calculation)     │
│  ├ bg_ledger_entries      (money movement)          │
│  ├ bg_webhook_events      (outbox for push notify)  │
│  ├ bg_capabilities        (capability contract)     │
│  └ bg_audit_logs          (audit trail)             │
│                                                     │
│  All tables include: org_id, project_id, api_key_id │
└─────────────────────────────────────────────────────┘
```

## Four Execution Modes

| Mode | API Entry | State Flow | Example |
|------|-----------|-----------|---------|
| **Sync** | `POST /v1/bg/responses` | queued → succeeded | LLM chat |
| **Stream** | `POST /v1/bg/responses` (mode=stream) | streaming → succeeded | LLM streaming |
| **Async** | `POST /v1/bg/responses` (mode=async) | accepted → queued → running → succeeded | Video processing |
| **Session** | `POST /v1/bg/sessions` | creating → active → idle → closed | Browser / Sandbox |

## State Machines

### Response State Machine

```
accepted → queued → running   → succeeded
                  → streaming → succeeded
                              → failed
                  → canceled
                  → expired
```

Terminal states: `succeeded`, `failed`, `canceled`, `expired`

### Attempt State Machine

```
dispatching → accepted → running → succeeded / failed / canceled / abandoned
```

One Response can have multiple Attempts (fallback). Only the final Attempt determines the Response terminal state.

### Session State Machine

```
creating → active → idle → closed / expired
                  → closed / expired / failed
```

## Capability Naming Convention

```
bg.<domain>.<action>.<tier>
```

Examples:
- `bg.llm.chat.standard` → gpt-5.4-mini
- `bg.llm.chat.pro` → gpt-5.4
- `bg.llm.chat.fast` → gpt-5.4-nano
- `bg.llm.reasoning.pro` → gpt-5.4-pro
- `bg.video.upscale.standard` → (async provider)
- `bg.browser.session.standard` → (session provider)

## Pricing Model: Dual-Layer, Capability-First

> **Implementation status legend**: ✅ Implemented · 🔜 Planned

### Design Principle

> Users pay for **capabilities**, not providers.
> The platform accounts costs by **providers** internally.

This means:
- **External (user-facing)**: a single price per capability tier, independent of which provider handles the request
- **Internal (platform accounting)**: per-provider cost tracking for margin analysis and settlement

### Why Not Provider-Based Pricing

Provider-based pricing (user sees `gpt-5.4-mini = $0.003/1K tokens`) violates the core abstraction:
- Exposes provider details, locking users to specific vendors
- Makes intelligent routing meaningless (users demand to pick the cheapest provider)
- Creates friction when replacing or adding providers

### Why Not Pure Capability Pricing Without Cost Tracking

Pure capability pricing without per-provider cost accounting creates uncontrolled margin risk:
- If `bg.llm.chat.standard` routes to Provider A ($0.003) vs Provider B ($0.008), the platform margin swings 2.6x on the same price

### The Dual-Layer Solution

**Layer 1 — Capability Pricing (user-facing)** ✅

Stored in `BgCapability` table. Users see one price per capability:

```
bg.llm.chat.fast     = $0.002 / 1K tokens
bg.llm.chat.standard = $0.005 / 1K tokens
bg.llm.chat.pro      = $0.015 / 1K tokens
bg.video.generate.standard = $0.05 / request
bg.sandbox.session.standard = $0.01 / minute
```

**Layer 2 — Provider Cost (platform-internal)** 🔜

Stored in `CapabilityBinding` table. Each binding records the provider's actual cost (currently `CapabilityBinding` exists but does not yet carry a `cost_per_unit` field):

```
bg.llm.chat.standard → gpt-5.4-mini    cost = $0.003 / 1K tokens
bg.llm.chat.standard → claude-4-haiku  cost = $0.004 / 1K tokens
bg.llm.chat.pro      → gpt-5.4         cost = $0.010 / 1K tokens
bg.llm.chat.pro      → claude-4-opus   cost = $0.012 / 1K tokens
```

**BillingRecord captures both layers** (partially ✅, target fields 🔜):

Current implemented fields: `amount`, `total_amount`, `provider_cost`, `platform_margin`.
Target schema uses `customer_charge` as the user-facing field name (rename planned):

```json
{
  "billing_mode": "hosted",
  "customer_charge": 0.005,
  "provider_cost": 0.003,
  "platform_margin": 0.002,
  "currency": "usd"
}
```

### BYO (Bring Your Own Key) Pricing 🔜

> BYO billing mode is recognised by `LookupPricing` (returns `UnitPrice: 0`), but the multi-mode fee structure below is not yet implemented.

When users provide their own provider credentials, the platform charges only a service fee:

```json
{
  "billing_mode": "byo",
  "customer_charge": 0.001,
  "provider_cost": 0.000,
  "platform_margin": 0.001
}
```

BYO pricing can be:
- Fixed fee per request (`per_call`) 🔜
- Percentage of estimated provider cost (`percentage`) 🔜
- Flat monthly fee (`flat_monthly`) 🔜

### Data Model

```
BgCapability (capability pricing) ✅
├── capability_name    "bg.llm.chat.standard"
├── billable_unit      "token"
├── unit_price         0.000005          ← user-facing price per unit
└── currency           "usd"

CapabilityBinding (provider cost) ✅ structure / 🔜 cost_per_unit field
├── capability_pattern "bg.llm.chat.standard"
├── adapter_name       "openai_native_ch1"
├── provider           "openai"
├── upstream_model     "gpt-5.4-mini"
├── cost_per_unit      0.000003          ← 🔜 provider cost per unit (not yet in struct)
└── weight / priority                    ← routing parameters ✅
```

### Billing Pipeline

```
Provider returns RawUsage
  → buildCanonicalUsage()          ✅ (normalize to billable units)
  → LookupPricing(capability)      ✅ (get user-facing price from BgCapability)
  → LookupProviderCost(binding)    🔜 (planned; currently folded into LookupPricing)
  → FinalizeBilling()              ✅ (single DB transaction)
    ├ INSERT bg_usage_records       ✅ (resource truth)
    ├ INSERT bg_billing_records     ✅ (amount + provider_cost + margin)
    └ INSERT bg_ledger_entries      ✅ (debit entry)
```

### Pricing Snapshot Immutability

At invocation time, the capability price and provider cost are frozen into a `PricingSnapshot` on the response record. This prevents price drift on long-running async tasks — the customer is always billed at the price that was active when they submitted the request.

- ✅ `PricingSnapshot` struct with `UnitPrice` (user-facing) — frozen at invocation in orchestrator & streaming
- 🔜 Add `CostPerUnit` (provider cost) to `PricingSnapshot` for full dual-layer snapshot

### Additional Notes

- Billing failure does NOT roll back response state (marked `billing_status=failed` for retry)
- Session billing aggregates `session_minutes` + `action_count` at close/expire
- Four-layer accounting separation is maintained: Response (execution) → Usage (resource) → Billing (pricing) → Ledger (money)

## Routing & Fallback

- 1:N capability → adapter mapping (priority + weight)
- Fallback loop: try next adapter on invoke error or `provider_unavailable`
- Safety constraint: no fallback after provider starts execution (prevents double-execution)

## Webhook Outbox

- `EnqueueWebhookEvent()` writes to `bg_webhook_events` table
- `BgWebhookWorker` delivers with exponential backoff (30s, 60s, 120s)
- State machine: pending → delivering → delivered / retrying → dead
- Triggered on: response terminal state, session close

## Multi-Tenant Identity

```
Organization (org_id)
  └── Project (project_id, via X-Project-Id header)
        └── API Key (api_key_id)
              └── End User (end_user_id, via metadata)
```

All core tables carry `org_id`, `project_id`, `api_key_id`, `billing_mode` (hosted/byo).

## Project Structure

```
new-api/
├── model/
│   ├── bg_response.go         # Response table + status machine + CAS
│   ├── bg_attempt.go          # Attempt table + CAS + pollable query
│   ├── bg_billing.go          # Usage + Billing + Ledger tables
│   ├── bg_session.go          # Session + SessionAction tables + CAS lock
│   ├── bg_webhook.go          # Webhook events table + status constants
│   ├── bg_capability.go       # Capability contract table
│   ├── bg_audit.go            # Audit log (async insert)
│   └── bg_response_test.go    # Model layer tests
│
├── dto/
│   ├── basegate.go            # API DTOs (Request/Response/Session/Model/Usage/Error)
│   └── basegate_test.go       # Serialization tests
│
├── controller/
│   ├── bg_responses.go        # POST/GET /v1/bg/responses, POST cancel
│   ├── bg_responses_test.go   # Controller integration tests
│   ├── bg_sessions.go         # POST/GET /v1/bg/sessions, POST action/close
│   ├── bg_sessions_test.go    # Session controller tests
│   └── model.go               # /v1/models with BaseGate capability injection
│
├── service/
│   ├── bg_orchestrator.go     # DispatchSync / DispatchAsync with fallback
│   ├── bg_orchestrator_test.go
│   ├── bg_state_machine.go    # ApplyProviderEvent + auto-advance + billing trigger
│   ├── bg_state_machine_test.go
│   ├── bg_streaming.go        # DispatchStream (SSE lifecycle)
│   ├── bg_billing_engine.go   # FinalizeBilling (txn) + LookupPricing
│   ├── bg_billing_engine_test.go
│   ├── bg_session_manager.go  # CreateSession / ExecuteAction / CloseSession
│   ├── bg_session_worker.go   # Idle/expire timeout enforcement
│   ├── bg_poll_worker.go      # Async task polling
│   ├── bg_outbox.go           # EnqueueWebhookEvent
│   ├── bg_webhook_worker.go   # Delivery with retry + backoff
│   └── bg_webhook_worker_test.go
│
├── relay/
│   ├── basegate/
│   │   ├── provider_adapter.go     # ProviderAdapter + SessionCapableAdapter interfaces
│   │   ├── adapter_registry.go     # 1:N registry + LookupAdapters/ByName
│   │   ├── legacy_wrapper.go       # Bridge existing TaskAdaptors
│   │   └── adapters/
│   │       ├── openai_llm_adapter.go      # Native OpenAI adapter (raw HTTP)
│   │       ├── kling_video_adapter.go     # Native Kling async video adapter (JWT auth)
│   │       ├── kling_jwt.go              # Kling JWT token generation
│   │       ├── e2b_sandbox_adapter.go    # Native E2B session adapter (gRPC-web)
│   │       └── *_test.go                 # Adapter unit + integration tests
│   ├── bg_register.go              # RegisterAllLegacyTaskAdaptors + RegisterNativeAdapters
│   └── common/
│       ├── canonical.go            # CanonicalRequest / AdapterResult / ID generators
│       └── sse_event.go            # SSE event type definitions
│
├── constant/
│   └── endpoint_type.go       # EndpointTypeBgResponses / BgSessions
│
└── router/
    └── relay-router.go        # 7 BaseGate routes registered
```

## Running Tests

```bash
# All BaseGate tests (model + dto + service + controller + adapter)
go test ./model/... ./dto/... ./service/... ./controller/... ./relay/basegate/... -count=1

# State machine tests only
go test ./service/... -v -run "TestApplyProviderEvent" -count=1

# Adapter tests (mock server)
go test ./relay/basegate/adapters/... -v -count=1

# E2B integration test (requires E2B_API_KEY env var)
E2B_API_KEY=... go test ./relay/basegate/adapters/... -run TestE2BIntegration -v -count=1

# Race detection
go test -race ./model/... ./service/... -count=1
```

## Development Timeline

| Phase | Commit | Content |
|-------|--------|---------|
| 1 | `0079ecb5` | Core models, state machine, database schemas |
| 2 | `20fb990e` | Unified gateway, billing engine, legacy bridge |
| 3 | `28967971` | Session management, lifecycle worker |
| 4 | `58a2bb2a` | Webhook outbox, SSE streaming, model discovery |
| 5 | `247acd77` `1f447865` `44138b6a` | Billing wiring, OpenAI adapter, routing, multi-tenant, audit |
| docs | `0bd5ac8c` | Architecture document, project structure refresh |
| 6 | `53335b2a` | Inbound callbacks, Usage API, HMAC webhooks, sandbox registry |
| 7 | `aea88e81` | Production Hardening (immutable pricing, idempotency, weighted routing, E2E tests) |
| 8 | `8e864bec` | Admin Dashboard (Usage/Response/Capabilities pages, admin APIs, i18n) |
| 9 | `93908749` | Native adapters: Kling video (async+JWT), E2B sandbox (session+gRPC-web) |
| 10 | *Current* | Circuit breaker, pre-authorization, project management |

## MVP Progress (Project Definition §16.3)

### Completed

| Requirement | Implementation | Status |
|---|---|---|
| `/v1/models` | `controller/model.go` — capability injection + dedup | ✅ |
| `/v1/responses` | `controller/bg_responses.go` — sync/async/stream | ✅ |
| `/v1/tasks/{id}` | `GET /v1/bg/responses/:id` (equivalent) | ✅ |
| API Key | Existing token system reused | ✅ |
| Provider adapter | `ProviderAdapter` interface + OpenAI/Kling/E2B native + 10 legacy bridge | ✅ |
| Routing strategy | 1:N registry + fallback loop | ✅ |
| usage event | `bg_usage_records` table + normalization | ✅ |
| ledger | `bg_ledger_entries` table + transactional writes | ✅ |
| webhook / polling | `bg_webhook_events` outbox + `bg_poll_worker` | ✅ |
| Basic rate limiting | Existing system reused | ✅ |
| Basic audit logging | `bg_audit_logs` table + async insert | ✅ |
| Tenant / Project fields | `org_id`/`project_id` activated on all tables | ✅ |
| State machine | Response + Attempt dual state machine with CAS | ✅ |
| Session capabilities | Session manager + action CAS lock + idle/expire worker | ✅ |
| Billing pipeline | Usage → Billing → Ledger in single transaction | ✅ |
| Callback inbound endpoint | `POST /v1/bg/callbacks/:response_id` + adapter validation | ✅ |
| PricingSnapshot | Immutability at invocation time to prevent async drift | ✅ |
| Idempotency payload | Deep equality conflict detection (`ErrIdempotencyConflict`) | ✅ |
| Async response metadata | `poll_url` added to `BaseGateResponse` for non-terminal states | ✅ |
| Usage query API | `GET /v1/bg/usage` with org scoping and date grouping | ✅ |
| Weighted routing | Router fallback dynamically follows `CapabilityBinding` weights | ✅ |
| Webhook Security | `X-BaseGate-Signature256` HMAC signing logic | ✅ |
| Admin Dashboard | Usage KPI cards, Response Explorer, Capabilities page | ✅ |
| Admin APIs | `/api/bg/` list/detail/stats endpoints with `AdminAuth` | ✅ |
| Kling Video Adapter | Native async adapter with JWT auth, progressive polling, callback | ✅ |
| E2B Sandbox Adapter | Native session adapter with gRPC-web code execution, billing | ✅ |
| Circuit Breaker | Three-state (closed/open/half-open) per-adapter with configurable threshold | ✅ |
| Pre-authorization | Estimate cost → reserve quota → settle at terminal state (refund/charge delta) | ✅ |
| Project Management | `bg_projects` CRUD via `/api/bg/projects` with admin dashboard | ✅ |

### Capability Validation (§16.2)

| Capability Type | Purpose | Status |
|---|---|---|
| **LLM** (sync + stream) | Validate sync and streaming | ✅ Verified with real OpenAI API |
| **Image/Video** (async + poll) | Validate async + metering + callback/poll | ✅ Kling native adapter with JWT, progressive polling, callback |
| **Browser/Sandbox** (session) | Validate session capabilities | ✅ E2B native adapter verified end-to-end (create → execute → close) |

### Not Yet Completed

#### P0 — Production Readiness

| Work Item | Effort | Description |
|---|---|---|
| E2E Testing Coverage | 1d | Expand session/streaming controller tests |

#### P1 — MVP Completeness

| Work Item | Effort | Description |
|---|---|---|
| Usage/Billing state machine | 1-2d | Add estimated/voided/refunded states |

#### P2 — Platform Capabilities

| Work Item | Effort | Description |
|---|---|---|
| BYO billing logic | 1-2d | Platform fee vs provider cost split |

#### P3 — Second Phase (§17)

| Work Item | Description |
|---|---|
| Capability → Tool projection | Auto-generate LLM-callable tools from capability schemas |
| Capability Policy | Per-org/project/key allow/deny rules |
| Routing Policy configuration | Per-tenant custom routing strategies |
| Marketplace | Third-party provider self-service onboarding |
| Complex pricing | Tiered, subscription + overage, credit system |

### Completion Summary

| Dimension | Progress | Notes |
|---|---|---|
| Core engine | 99% | Circuit breaker + pre-auth complete the production-critical path |
| API protocol | 95% | Admin + Usage + Project CRUD; missing only capability policy API |
| Provider adapters | 90% | LLM (OpenAI), Video (Kling), Sandbox (E2B) native + 10 legacy bridge |
| Multi-tenant governance | 55% | Project CRUD + admin dashboard + scoped usage/billing APIs |
| Routing & scheduling | 90% | Weighted routing + circuit breaker + fallback; missing routing policy config |
| Billing completeness | 80% | Pre-auth + settlement + cross-tenant scoping; missing BYO/tiered pricing |
| MVP capability validation | 95% | All 3 capability types verified; pre-auth + circuit breaker hardened |
