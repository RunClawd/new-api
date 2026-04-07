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

## Billing Pipeline

```
Provider returns RawUsage
  → buildCanonicalUsage()     (normalize to billable units)
  → FinalizeBilling()         (single DB transaction)
    ├ INSERT bg_usage_records
    ├ INSERT bg_billing_records  (quantity × unit_price)
    └ INSERT bg_ledger_entries   (debit entry)
```

- Pricing resolved via `LookupPricing()` bridging to existing `ratio_setting`
- Billing failure does NOT roll back response state (marked `billing_status=failed` for retry)
- Session billing aggregates `session_minutes` + `action_count` at close/expire

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
│   │       └── openai_llm_adapter_test.go
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
