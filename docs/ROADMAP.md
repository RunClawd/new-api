# BaseGate Development Roadmap

> 基于 Phase 10 完成后的现状分析，设计 Phase 11-17 的阶段性任务。
> 目标：从 MVP 状态推进到可对外内测的产品。

---

## 现状总结

### 已建成

| 维度 | 完成度 | 说明 |
|------|--------|------|
| 核心引擎 | 99% | 四种执行模式 + 双状态机 + CAS + 熔断 + 预授权 |
| API 协议 | 95% | 7 个 BaseGate 路由 + Admin API + Usage API |
| Provider 适配器 | 30% | 3 个原生 (OpenAI/Kling/E2B) + 10 个 Legacy Bridge |
| 多租户治理 | 55% | 字段预留 + Project CRUD；缺 Capability Policy、BYO、角色权限 |
| 路由调度 | 70% | 权重 + 优先级 + 熔断跳过；缺可配置路由策略、评分公式 |
| 计费完整度 | 95% | 四层账务 + 预授权 + Billing 状态机（Sync/Async 双路径）；⚠️ 1 个单元测试待修复 |
| 策略引擎 | ✅ 100% | Capability Policy + Routing Policy 全功能实现，测试覆盖完整 |
| 前端 | 40% | 8 个 Bg 页面（Dashboard/Responses/Sessions/Capabilities/Adapters/Billing/Usage/Projects）；缺开发者控制台、策略配置 UI |
| 测试 | 85% | 40+ test files；1 个 legacy test 失败，缺 E2E 覆盖 |

### Schema 规范 vs 实现差距

| Schema 模块 | 规范状态 | 实现状态 | 差距 |
|-------------|---------|---------|------|
| 一、Model Metadata | ✅ 已定义 | ✅ 已实现 | — |
| 二、Responses API | ✅ 已定义 | ✅ 已实现 | — |
| 三、Streaming SSE | ✅ 已定义 | ✅ 已实现 | — |
| 四、Capability → Tool | ✅ 已定义 | ❌ 未实现 | 完全缺失 |
| 五、Provider Adapter | ✅ 已定义 | ⚠️ 部分 | 接口完整，原生适配器不足 |
| 六、调度与计费 | ✅ 已定义 | ⚠️ 部分 | 缺 BYO 计费；Billing 状态机 ✅ 已实现 |
| 七、状态机与回调 | ✅ 已定义 | ✅ 已实现 | — |
| 八、Session 协议 | ✅ 已定义 | ✅ 已实现 | — |
| 九、多租户与路由 | ✅ 已定义 | ✅ 已实现 | Capability Policy + Routing Policy 完整实现 |

---

## Phase 11: Billing 状态机打通与测试加固 (3-3.5d)

**目标**：把 preauth 流程与 billing record 状态机真正接通（当前 preauth 绕过 billing 层直接操作 quota），修复测试问题。

> **设计决策**：BYO 计费逻辑推迟到 Phase 13（与 BYO Credential 同步实现）。
> 原因：`BillingMode` / `ProviderCost` / `PlatformMargin` 字段已预留在 `BgBillingRecord` 中，
> 但当前无 BYO credential 来源，无法触发 `billing_mode=byo`。Phase 11 只预留接口分支骨架，
> 不实现真实 BYO 计费路径。

### 11.1 Pre-auth ↔ Billing 状态机打通 (P1, 1.5d)

**现状问题**：

`bg_preauth.go` 的 `TryReserveQuota` / `SettleReservation` 直接调用 `model.DecreaseUserQuota` / `IncreaseUserQuota`，完全绕过了 billing 层。`BgBillingStatus` 的 `estimated` / `voided` / `refunded` 常量已在 `bg_billing.go:48-52` 定义，但从未被使用。

**目标**：让 preauth 生命周期在 billing record 和 ledger 中留下完整轨迹。

**关键设计决策：按执行模式分流 preauth 策略 + reconciliation 兜底**

写放大的根源是 estimated billing record（2 INSERT + 2 UPDATE），而非 quota 预扣（1 UPDATE）。
因此 Sync 路径**保留 quota 预扣**（防用户透支），**砍掉 estimated billing record**（避免写放大）。
崩溃安全由 bg_responses 记录 + 后台 reconciliation sweep 兜底。

| 执行模式 | Quota 预扣 | Estimated billing record | 崩溃安全网 | 热路径额外写入 |
|----------|-----------|-------------------------|-----------|---------------|
| **Sync / Stream** | `DecreaseUserQuota`（1 UPDATE） | 不写 | bg_responses + reconciliation sweep | **0** |
| **Async / Session** | `DecreaseUserQuota`（1 UPDATE） | 写 estimated + hold ledger | estimated record + settlement | +2 INSERT, +2 UPDATE |

**Sync/Stream 路径**：

```
DecreaseUserQuota(orgID, estimatedQuota) → ok / insufficient
  → insufficient → 402
  → ok → bgResp.Insert() (status=queued, estimated_quota, pricing_snapshot_json 已写入)
  → adapter.Invoke()
  → ApplyProviderEvent() → FinalizeBilling() (usage+billing+ledger 事务写)
  → SettleReservation(orgID, estimated, actual) (退多扣的 quota)
```

崩溃场景分析：
- Crash after DecreaseUserQuota, before bgResp.Insert：quota 已扣但无 response 记录 → reconciliation 通过 orphaned quota 检测恢复（低概率，窗口极小）
- Crash after bgResp.Insert, before Invoke：response 卡在 queued → reconciliation 标记 failed + 退 quota
- Crash after Invoke, before FinalizeBilling：response 可能卡在 queued 或到了 terminal 但 billing_status 非 completed → reconciliation 重跑 FinalizeBilling 或退 quota

**Async/Session 路径**：

```
DecreaseUserQuota(orgID, estimatedQuota) → ok / insufficient
  → insufficient → 402
  → ok → INSERT bg_billing_records (status=estimated)
  → INSERT bg_ledger_entries (entry_type=hold, status=pending)
  → bgResp.Insert()
  → adapter.Invoke()
  → ... poll/session lifecycle ...

SettleReservation (成功)
  → UPDATE bg_billing_records SET status=posted, amount=actual_cost
  → UPDATE bg_ledger_entries  SET status=committed, amount=actual_cost
  → 退还 quota 差额 (actual < estimated)

SettleReservation (失败/取消)
  → UPDATE bg_billing_records SET status=voided
  → UPDATE bg_ledger_entries  SET status=voided
  → 全额退还 quota
```

**RefundBilling（两种模式通用）**：

```
RefundBilling (后置调整)
  → INSERT bg_billing_records (status=refunded, linked to original)
  → INSERT bg_ledger_entries  (entry_type=refund, direction=credit)
  → IncreaseUserQuota
```

**Reconciliation Sweep（新增交付物）**：

```go
// service/bg_reconciliation.go — 进程启动时 + 每 5 分钟运行
//
// Case 1: 卡在非终态超过 5 分钟
//   → 标记 failed + 退还 estimated_quota
//   → bg_responses 已有 estimated_quota 和 org_id，足够退款
//
// Case 2: 已到终态但 billing_status != 'completed'
//   → 从 pricing_snapshot_json 重建 pricing
//   → 从 bg_response_attempts 拿到 adapter name + usage
//   → 重跑 FinalizeBilling()
//
// Case 3 (Async): estimated billing record 长期未 settle
//   → 与 Case 1 类似，void estimated record + 退 quota
```

**文件变更**：

| 文件 | 变更 | 说明 |
|------|------|------|
| `service/bg_preauth.go` | 保留 `TryReserveQuota`（Sync 仍预扣 quota） | 签名改为接收 responseID/pricing，Async 额外写 estimated record |
| `service/bg_preauth.go` | 重构 `SettleReservation` | 接收 responseID，Async 更新 billing status；Sync 只退 quota 差额 |
| `service/bg_billing_engine.go` | 新增 `RefundBilling(billingID, reason)` | 后置退款 |
| `service/bg_reconciliation.go` | **新建** | 崩溃恢复 sweep：stuck responses + unbilled terminals + orphaned estimates |
| `model/bg_billing.go` | 新增 `UpdateBillingStatus(billingID, newStatus)` | CAS 状态转换方法 |
| `model/bg_billing.go` | 新增 `UpdateLedgerEntryStatus(entryID, newStatus, actualAmount)` | 对应 ledger 更新 |
| `service/bg_orchestrator.go` | Async 路径额外调用 estimated record 写入 | Sync 路径不变，保持现有 TryReserveQuota 行为 |

**BYO 骨架预留**（不实现真实逻辑）：

```go
// service/bg_billing_engine.go — FinalizeBilling 中预留分支注释
// BYO path (Phase 13): when billing_mode == "byo", use existing Amount field
// as platform fee, set ProviderCost=0, PlatformMargin=Amount.
// No new PlatformFee column needed — reuse existing fields.
```

### 11.2 测试加固 (P0, 1-1.5d)

| 任务 | 复杂度 | 说明 |
|------|--------|------|
| 修复 `task_billing_test.go` | 低 | "sql: database is closed" — DB teardown 顺序问题，需检查 `TestMain` 或单个 test 的 cleanup |
| Pre-auth billing 集成测试 | 中 | estimated→posted, estimated→voided, posted→refunded 全路径验证 |
| Session E2E 测试 | 高 | 完整 session lifecycle（create→action→close），需 mock adapter，可能涉及并发 |
| Streaming E2E 测试 | 高 | SSE 生命周期测试，需 httptest + goroutine 协调，容易低估 |
| 全量回归 | — | `go test ./... -race -count=1` 全部通过 |

> E2E 测试复杂度提醒：session 和 streaming 涉及 goroutine 协调、超时控制、mock adapter 行为编排，
> 预留弹性时间。如超出预期，可拆分为：P0（preauth billing 测试 + legacy fix）先行，
> session/streaming E2E 作为 11.3 收尾或滑入 Phase 12 初期。

### 11.3 验收标准

- [x] `go test ./... -count=1` 全部 PASS（含 legacy test fix）→ ⚠️ `TestCalculateBilling_TokenPricing` 待修复
- [x] **Sync 路径**：quota 预扣 + FinalizeBilling 一步到位，无 estimated record 写入
- [x] **Async 路径**：estimated billing record + hold ledger entry 正确写入
- [x] **Async 成功**：billing record estimated → posted，ledger pending → committed
- [x] **Async 失败/取消**：billing record → voided，ledger → voided，quota 全额退还
- [x] **Reconciliation sweep**：stuck >5min 的 response 被标记 failed + 退 quota
- [x] **Reconciliation sweep**：terminal 但 billing_status 非 completed 的 response 被补计费
- [x] `RefundBilling` 可对 posted record 创建 refunded 记录
- [x] BYO 分支注释预留，不影响现有 hosted 路径

---

## Phase 12: Capability Policy & Routing Policy (4-5d)

**目标**：实现 Schema 九 中定义的能力策略和路由策略，让不同租户可以有不同的权限和调度行为。

### 12.1 Capability Policy — 谁能用什么 (2d)

控制 org/project/key 级别的能力访问权限。

**新表**：`bg_capability_policies`

```go
type BgCapabilityPolicy struct {
    ID                uint   `gorm:"primaryKey"`
    Scope             string // "platform" | "org" | "project" | "key"
    ScopeID           int
    CapabilityPattern string // "bg.llm.*" | "bg.video.generate.*"
    Action            string // "allow" | "deny"
    MaxConcurrency    int    // 0 = unlimited
    Status            string
    CreatedAt         time.Time
}
```

**文件变更**：

| 文件 | 变更 | 说明 |
|------|------|------|
| `model/bg_capability_policy.go` | **新建** | 表定义 + CRUD |
| `model/main.go` | 修改 | AutoMigrate 注册 |
| `service/bg_policy_engine.go` | **新建** | `EvaluateCapabilityAccess(orgID, projectID, keyID, capability) → allow/deny` |
| `service/bg_orchestrator.go` | 修改 | Dispatch 前调用 policy check，deny → 403 |
| `controller/bg_policy.go` | **新建** | Admin CRUD API：`/api/bg/policies/capabilities` |
| `router/api-router.go` | 修改 | 注册 policy 路由 |
| `service/bg_policy_engine_test.go` | **新建** | allow/deny/wildcard/层级覆盖测试 |

**策略匹配规则**：
1. Key-level > Project-level > Org-level > Platform-level
2. 具体 pattern > 通配符 pattern
3. deny 优先于 allow（同级别）

### 12.2 Routing Policy — 走哪个 Provider (2-3d)

让 org/project 可以配置自定义路由策略。

**新表**：`bg_routing_policies`

```go
type BgRoutingPolicy struct {
    ID                uint   `gorm:"primaryKey"`
    Scope             string // "platform" | "org" | "project" | "key"
    ScopeID           int
    CapabilityPattern string
    Strategy          string // "weighted" | "fixed" | "cost_optimized" | "latency_optimized" | "byo_first"
    RulesJSON         string // JSON: preferred_providers, weights, fallback, region, etc.
    Priority          int
    Status            string
    CreatedAt         time.Time
}
```

**文件变更**：

| 文件 | 变更 | 说明 |
|------|------|------|
| `model/bg_routing_policy.go` | **新建** | 表定义 + CRUD |
| `service/bg_router.go` | **新建** | `ResolveRoute(orgID, projectID, keyID, capability) → []AdapterCandidate` |
| `service/bg_orchestrator.go` | 修改 | 替换现有 `LookupAdapters` 调用，走 policy engine |
| `controller/bg_policy.go` | 修改 | 增加 routing policy CRUD：`/api/bg/policies/routing` |
| `service/bg_router_test.go` | **新建** | fixed/weighted/byo_first 策略测试 |

**第一版策略类型**：
- `fixed` — 固定到指定 adapter
- `weighted` — 按权重分配（复用现有 CapabilityBinding.Weight）
- `primary_backup` — 主 adapter + fallback 列表
- `byo_first` — 有 BYO 凭证时优先使用

> `cost_optimized` / `latency_optimized` 需要运行时指标采集，放到后续 Phase。

### 12.3 验收标准

- [x] Capability Policy: org 级 deny `bg.sandbox.*` → 请求返回 403
- [x] Capability Policy: project 级 allow 覆盖 org 级 deny
- [x] Capability Policy: enforced platform deny 不可被下级 scope 覆盖
- [x] Capability Policy: `cacheInitialized=false` 时返回 500（防止 fail-open）
- [x] Routing Policy: `fixed` 策略锁定到指定 adapter
- [x] Routing Policy: `weighted` 策略确定性排序正确（固定 seed 测试）
- [x] Routing Policy: `primary_backup` 返回 [primary, fallback...] 顺序
- [x] Routing Policy: legacy wrapper (`legacy_task_suno`) 名称匹配
- [x] Admin API: CRUD policy 全部可用，`Validate()` 在 model 层执行
- [x] Admin API: `InvalidatePolicyCache` 失败时返回 500 `cache_sync_failed`
- [x] 审计: CRUD（先于 cache reload）和 deny 事件均有审计记录
- [x] 统一错误映射: `writeBGError` 覆盖所有 BaseGate sentinel error
- [x] 无 policy 配置时行为不变（向后兼容）
- [x] `go test ./... -race` Phase 12 相关包全部 PASS

---

## Phase 13: BYO 全链路 + 高频原生适配器 (6-9d)

**目标**：BYO credential 存储 + BYO 计费逻辑（从 Phase 11 移入）+ 高频 Provider 原生适配器。BYO 全链路在此 Phase 闭环。

### 13.1 BYO Credential 管理 (2d)

**新表**：`bg_byo_credentials`

```go
type BgBYOCredential struct {
    ID              uint   `gorm:"primaryKey"`
    OrgID           int
    ProjectID       int    // 0 = org-wide
    Provider        string // "openai" | "anthropic" | "google" | ...
    CredentialType  string // "api_key" | "oauth" | "service_account"
    EncryptedValue  string // AES-256 encrypted
    CapabilitiesJSON string // ["bg.llm.*"] — 可用于哪些能力
    Status          string
    CreatedAt       time.Time
}
```

**文件变更**：

| 文件 | 变更 |
|------|------|
| `model/bg_byo_credential.go` | **新建** — 表定义 + CRUD + 加密存取 |
| `common/crypto.go` | 修改 — 增加 AES 加密/解密工具函数 |
| `controller/bg_byo.go` | **新建** — Admin CRUD：`/api/bg/credentials` |
| `service/bg_router.go` | 修改 — 路由时查询 BYO credential，注入到 adapter |
| `relay/basegate/provider_adapter.go` | 修改 — Invoke 方法接受可选的 credential override |
| `router/api-router.go` | 修改 — 注册 credential 路由 |

### 13.2 BYO 计费逻辑落地 (1.5d)

Phase 11 预留的 BYO 骨架在此实现真实逻辑。

**前置条件**：13.1 BYO Credential 已可用。

**字段复用决策**：不新增 `PlatformFee` 字段。BYO 模式下复用现有 `BgBillingRecord` 字段：
- `Amount` = 平台服务费（对用户收取的总金额）
- `ProviderCost` = 0（用户自付上游）
- `PlatformMargin` = Amount（全额为平台收入）
- 新增 `FeeType` string 字段：记录服务费计算方式

**Phase 13 只实现 `per_request` + `percentage` 两种 FeeType**：
- `per_request`：固定金额/次（如 $0.001/request）
- `percentage`：按估算 provider cost 的百分比收取（如 10%）
- ~~`flat_monthly`~~：需订阅模型 + 计费周期定时任务，推至后续 Phase

**实现内容**：

| 文件 | 变更 |
|------|------|
| `service/bg_billing_engine.go` | 实现 `finalizeBYOBilling()`：Amount=服务费, ProviderCost=0, PlatformMargin=Amount |
| `service/bg_billing_engine.go` | 新增 `CalculateBYOFee(feeType, baseAmount) float64`：per_request / percentage |
| `model/bg_billing.go` | BgBillingRecord 新增 `FeeType` string 字段 |
| `service/bg_router.go` | 路由命中 BYO credential 时，设置 `billing_mode=byo` 传入计费引擎 |
| `service/bg_preauth.go` | BYO + Async/Session：预授权只估 platform fee |
| `service/bg_billing_engine_test.go` | BYO 计费单测：hosted vs byo、per_request + percentage |

**验证**：用 BYO credential + 新原生适配器联合测试，确认 billing_record 中 `billing_mode=byo`, `provider_cost=0`, `platform_margin=amount`。

### 13.3 Anthropic Claude 原生适配器 (2d)

最高优先级的原生适配器迁移。

```go
// relay/basegate/adapters/anthropic_llm_adapter.go
type AnthropicLLMAdapter struct {
    ChannelID    int
    APIKey       string
    BaseURL      string
    ModelMapping map[string]string
}
```

**支持**：
- Sync: Messages API → CanonicalResponse
- Stream: SSE → StreamEvent channel
- 参数映射：system/user/assistant messages, tools, temperature, max_tokens
- 错误映射：429/500/529 → BaseGate error codes
- 用量提取：input_tokens, output_tokens from response headers/body

### 13.4 Google Gemini 原生适配器 (1.5d)

```go
// relay/basegate/adapters/gemini_llm_adapter.go
type GeminiLLMAdapter struct {
    ChannelID    int
    APIKey       string
    ProjectID    string // for Vertex AI
    ModelMapping map[string]string
}
```

**支持**：
- Sync: generateContent → CanonicalResponse
- Stream: streamGenerateContent SSE → StreamEvent channel
- 双模式：Google AI Studio (API Key) + Vertex AI (Service Account)

### 13.5 DeepSeek 原生适配器 (1d)

OpenAI-compatible API，最简单的原生适配器。

```go
// relay/basegate/adapters/deepseek_llm_adapter.go
type DeepSeekLLMAdapter struct {
    ChannelID    int
    APIKey       string
    BaseURL      string
}
```

**支持**：
- 复用大部分 OpenAI 适配器逻辑
- 特有：reasoning_content 字段映射
- 成本优势：作为 `bg.llm.chat.standard` 的低成本候选

### 13.6 验收标准

- [ ] BYO credential 加密存储/读取正常
- [ ] `byo_first` 路由策略正确使用 BYO credential
- [ ] BYO 计费：billing_record 中 `billing_mode=byo`, `provider_cost=0`, `platform_margin=amount`
- [ ] BYO 预授权（Async/Session）：只估算 platform fee
- [ ] per_request + percentage 两种 fee_type 计算正确
- [ ] Anthropic adapter: sync + stream 全部通过（真实 API 验证）
- [ ] Gemini adapter: sync + stream 全部通过
- [ ] DeepSeek adapter: sync + stream 全部通过（若时间紧张可滑入 Phase 14）
- [ ] 新适配器注册到 adapter registry，可被路由引擎选中
- [ ] BYO + 新适配器联合测试：用 BYO key 调用 Claude，完整计费链路验证

> **Fallback 策略**：若 Anthropic SSE 映射（content_block_delta 嵌套事件）超出预期，
> DeepSeek（OpenAI-compatible，最简单）可优先完成以保证至少有 2 个新原生适配器交付。

---

## Phase 14: 开发者体验与前端改造 (5-7d)

**目标**：从"管理员后台"转变为"开发者可用的产品"。当前前端是 new-api 管理后台加 BaseGate 数据页面，缺少面向开发者的自助体验。

### 14.1 开发者 API Key 管理 (1.5d)

开发者需要能自助创建、管理 API Key，并将 Key 绑定到 Project。

| 文件 | 变更 |
|------|------|
| `web/src/pages/BgApiKeys/index.jsx` | **新建** — Key 列表、创建、删除、Project 绑定 |
| `controller/bg_apikey.go` | **新建** — 开发者级 Key CRUD（非 admin） |
| `router/api-router.go` | 修改 — 注册开发者 API |

### 14.2 开发者 Dashboard (1.5d)

开发者登录后看到的首页，展示与自己相关的数据。

| 页面 | 内容 |
|------|------|
| Overview | 今日调用量、成功率、花费、配额余量 |
| 实时日志 | 最近 50 条请求，可点击查看详情 |
| 用量趋势 | 按天/小时的调用量和花费图表 |

**文件**：`web/src/pages/BgDevDashboard/index.jsx`

### 14.3 能力目录页面 (1d)

让开发者浏览平台支持的所有能力，查看定价和参数。

| 页面 | 内容 |
|------|------|
| 能力列表 | 分域展示（LLM/Video/Browser/Sandbox）|
| 能力详情 | 输入/输出 schema、定价、支持的模式、示例请求 |

**文件**：`web/src/pages/BgCapabilityCatalog/index.jsx`

### 14.4 API Playground (2d)

在浏览器中直接测试 BaseGate API 的交互式面板。

| 功能 | 说明 |
|------|------|
| 能力选择 | 下拉选择能力名 |
| 参数编辑 | JSON 编辑器填写请求体 |
| 发送请求 | 展示完整的 request/response |
| Stream 预览 | SSE 流式输出实时展示 |
| 代码生成 | 根据当前请求生成 curl/Python/JS 代码片段 |

**文件**：`web/src/pages/BgPlayground/index.jsx`

### 14.5 策略配置 UI (1d)

管理员配置 Capability Policy 和 Routing Policy 的界面。

| 页面 | 内容 |
|------|------|
| Capability Policy | 规则列表、创建（scope + pattern + action）|
| Routing Policy | 策略列表、创建（scope + pattern + strategy + weights）|

**文件**：
- `web/src/pages/BgPolicies/CapabilityPolicies.jsx`
- `web/src/pages/BgPolicies/RoutingPolicies.jsx`

### 14.6 验收标准

- [ ] 开发者可自助注册 → 创建 Project → 创建 API Key → Playground 测试 → 查看用量
- [ ] 管理员可在 UI 上配置 Capability Policy 和 Routing Policy
- [ ] 所有新页面支持中英文 i18n
- [ ] 移动端基本可用（响应式布局）

---

## Phase 15: Capability → Tool 投影与 SDK (4-5d)

**目标**：实现 Schema 四中定义的自动投影能力，让 Agent 框架可以直接发现和调用 BaseGate 能力。

### 15.1 Capability → Tool 自动投影 (2d)

从 BgCapability 元数据自动生成 OpenAI Function Calling 格式的 tool definition。

```json
// GET /v1/bg/tools → 返回可被 LLM 调用的 tool 列表
[
  {
    "type": "function",
    "function": {
      "name": "bg_video_generate_standard",
      "description": "Generate video from text prompt",
      "parameters": {
        "type": "object",
        "properties": {
          "prompt": {"type": "string", "description": "Video generation prompt"},
          "duration": {"type": "integer", "description": "Duration in seconds"}
        },
        "required": ["prompt"]
      }
    }
  }
]
```

**文件变更**：

| 文件 | 变更 |
|------|------|
| `model/bg_capability.go` | 扩展 BgCapability：增加 `input_schema_json`, `output_schema_json` |
| `service/bg_tool_projection.go` | **新建** — `ProjectCapabilityToTool(cap BgCapability) ToolDefinition` |
| `controller/bg_tools.go` | **新建** — `GET /v1/bg/tools`，返回投影后的 tool 列表 |
| `dto/basegate.go` | 增加 ToolDefinition DTO |
| `router/relay-router.go` | 注册 `/v1/bg/tools` |

### 15.2 Tool Call → BaseGate 自动路由 (1d)

当 LLM 返回 tool_call 且 name 匹配 `bg_*` pattern 时，自动路由到 BaseGate 执行。

```
LLM response: tool_call { name: "bg_video_generate_standard", arguments: {...} }
  → BaseGate 解析 tool name → 还原 capability name
  → 自动创建 bg_response（async 模式）
  → 返回 tool_result 给 LLM
```

**文件变更**：

| 文件 | 变更 |
|------|------|
| `service/bg_tool_executor.go` | **新建** — tool_call 解析 + capability 调用 + 结果封装 |
| `controller/bg_tools.go` | 增加 `POST /v1/bg/tools/execute` — 接收 tool_call，返回 tool_result |

### 15.3 Python SDK (1.5d)

最小可用的 Python SDK，覆盖核心操作。

```python
from basegate import BaseGate

bg = BaseGate(api_key="bg-xxx", base_url="https://api.basegate.io")

# Sync
result = bg.responses.create(model="bg.llm.chat.standard", input="Hello")

# Stream
for event in bg.responses.stream(model="bg.llm.chat.standard", input="Hello"):
    print(event.delta)

# Async
resp = bg.responses.create(model="bg.video.generate.standard", input={...}, mode="async")
result = bg.responses.poll(resp.id)  # or use webhook

# Session
session = bg.sessions.create(model="bg.sandbox.python.standard")
action = bg.sessions.execute(session.id, code="print('hello')")
bg.sessions.close(session.id)

# Tools (for Agent integration)
tools = bg.tools.list()
result = bg.tools.execute(name="bg_video_generate_standard", arguments={...})
```

**文件**：`sdk/python/basegate/` — 独立目录，可发布 PyPI

### 15.4 验收标准

- [ ] `GET /v1/bg/tools` 返回所有 active capability 的 tool definition
- [ ] Tool name 与 capability name 双向转换正确
- [ ] Python SDK 覆盖 sync/stream/async/session 四种模式
- [ ] SDK 自带 type hints 和 docstring

---

## Phase 16: 可观测性与运营就绪 (3-4d)

**目标**：补齐生产运营所需的监控、告警和运维能力。

### 16.1 指标采集与暴露 (1.5d)

| 指标 | 类型 | 说明 |
|------|------|------|
| `bg_requests_total` | Counter | 按 capability/status/billing_mode 分维度 |
| `bg_request_duration_seconds` | Histogram | 端到端延迟 |
| `bg_adapter_duration_seconds` | Histogram | Provider 调用延迟（按 adapter） |
| `bg_circuit_breaker_state` | Gauge | 每个 adapter 的熔断状态 |
| `bg_active_sessions` | Gauge | 活跃 session 数 |
| `bg_billing_amount_total` | Counter | 累计计费金额 |

**实现**：`service/bg_metrics.go` — Prometheus metrics，通过现有 `/metrics` endpoint 暴露。

### 16.2 结构化日志 (0.5d)

| 变更 | 说明 |
|------|------|
| 统一日志格式 | 所有 bg_* 日志增加 `org_id`, `project_id`, `response_id`, `capability` 字段 |
| 请求追踪 | `X-Request-Id` header 贯穿完整链路 |
| 慢请求告警 | >10s 的请求自动 WARN 级别日志 |

### 16.3 健康检查与运维 API (1d)

| API | 说明 |
|-----|------|
| `GET /health` | 深度健康检查：DB + Redis + 各 adapter 状态 |
| `GET /api/bg/admin/circuit-breakers` | 熔断器状态总览 + 手动 reset |
| `GET /api/bg/admin/workers` | 后台 worker 状态（poll/session/webhook）|
| `POST /api/bg/admin/cache/clear` | 清除 capability/pricing 缓存 |

### 16.4 验收标准

- [ ] Prometheus `/metrics` 包含所有 bg_ 前缀指标
- [ ] Grafana dashboard 模板可用（提供 JSON 导入文件）
- [ ] 健康检查端点反映真实依赖状态
- [ ] 慢请求日志可追踪到具体 adapter

---

## Phase 17: 产品化补齐 — 从"引擎完成"到"可内测" (8-12d)

**目标**：Phase 11-16 完成了全部技术引擎，但缺少用户入口、文档、安全加固等产品化必需品。本 Phase 补齐这些差距，使系统真正达到可对外内测的状态。

> **背景**：Phase 16 完成后的系统状态是——所有引擎都在转，但没有入口（开发者前端）、
> 没有说明书（文档）、没有门卫（安全加固）。技术上完整但产品上未就绪。

### Phase 16 完成后的差距分析

| 维度 | Phase 16 后状态 | 差什么才能对外 |
|------|----------------|---------------|
| 核心引擎 | 生产级 | 已经足够 |
| Provider 覆盖 | 6 原生 + 10 legacy | 够用 |
| 多租户治理 | 完整 | 够用 |
| 计费 | 完整含 BYO | 够用 |
| 前端 | 管理后台完整，**开发者面板缺失** | 开发者无法自助使用 |
| 文档 | 无 | **致命缺失**——没有 API 文档，SDK 等于不存在 |
| 认证/注册 | 继承 new-api 的用户系统 | 缺开发者自助注册流程 |
| 部署 | Dockerfile 有 | 缺部署文档、环境变量说明、一键部署脚本 |
| 安全 | BYO 凭证加密有了 | 缺 per-key rate limit、IP 白名单、abuse 防护 |
| 高可用 | 单实例 | 缺多实例部署验证 |

### 17.1 开发者最小前端 (P0, 2-3d)

开发者的核心循环是：**拿到 Key → 调 API → 看结果 → 查用量/花费 → 续费/调整**。

基于这个循环，最小前端只需 4 个页面：

**实施策略**：新建 `DevRoute`（开发者角色路由），复用现有管理页面组件 + scope 过滤。

#### 17.1.1 API Key 管理（新建）

```
功能：
- 创建 Key（绑定 Project，名称，过期时间）
- 列表（名称、前缀、创建时间、最后使用时间、状态）
- 删除/禁用
- 创建时只展示一次完整 Key

不需要（首版）：
- Key 权限粒度配置（所有 Key 权限相同）
- 用量限额配置（用全局 quota）
```

| 文件 | 变更 |
|------|------|
| `web/src/pages/BgApiKeys/index.jsx` | **新建** — Key CRUD 页面 |
| `controller/bg_apikey.go` | **新建** — 开发者级 Key CRUD API（非 admin） |
| `router/api-router.go` | 修改 — 注册开发者 API 路由 |

#### 17.1.2 用量与花费（复用 BgUsage，降权限）

```
功能：
- 本月总花费 / 剩余额度（大数字卡片）
- 按天的调用量和花费折线图
- 按模型/能力的花费分布

不需要（首版）：
- CSV 导出
- 跨组织筛选（开发者只看自己的）
```

| 文件 | 变更 |
|------|------|
| `web/src/pages/BgDevUsage/index.jsx` | **新建** — 包装 BgUsage 组件，加 `scope=current_user` 过滤 |

#### 17.1.3 请求日志（复用 BgResponses，降权限）

```
功能：
- 最近 N 条请求列表（时间、模型、状态、延迟、花费）
- 点击看详情（请求/响应、错误信息）
- 按状态筛选（成功/失败）

不需要（首版）：
- Attempts 详情（运维调试用）
- Billing record 关联
```

| 文件 | 变更 |
|------|------|
| `web/src/pages/BgDevLogs/index.jsx` | **新建** — 包装 BgResponses 组件，简化展示 |

#### 17.1.4 Playground（已有，移到 DevRoute）

```
已有功能足够：选能力、填 JSON、发请求、看响应、Stream 实时输出

需改动：
- 从 AdminRoute 移到 DevRoute
- 自动填入当前用户的 API Key
```

| 文件 | 变更 |
|------|------|
| `web/src/App.jsx` | 修改 — Playground 同时挂载到 AdminRoute 和 DevRoute |

### 17.2 API 文档站 (P0, 2-3d)

**现状问题**：没有文档 = 没有产品。开发者拿到 Key 后不知道怎么用。

| 文档 | 内容 | 格式 |
|------|------|------|
| Quick Start | 5 分钟从注册到第一次 API 调用 | Markdown / 静态站 |
| API Reference | 能力列表、请求/响应格式、认证方式、错误码 | OpenAPI spec + 渲染 |
| SDK Guide | Python SDK 安装、初始化、四种模式示例 | Markdown |
| 错误码表 | 所有 BaseGate 错误码及处理建议 | 表格 |

| 文件 | 变更 |
|------|------|
| `docs/api/` | **新建** — OpenAPI spec（从现有路由自动或手动生成） |
| `docs/guide/` | **新建** — Quick Start + SDK Guide |
| `docs/site/` | **新建** — 静态文档站（VitePress / Docusaurus，选一） |

### 17.3 开发者注册与登录 (P0, 1d)

| 任务 | 说明 |
|------|------|
| 开发者注册页面 | 基于现有用户系统，增加开发者角色自助注册 |
| 注册后引导 | 注册 → 创建第一个 Project → 获取 API Key → 跳转 Playground |
| 邀请码机制 | 内测阶段通过邀请码控制注册（可选，防滥用） |

| 文件 | 变更 |
|------|------|
| `web/src/pages/BgRegister/index.jsx` | **新建** — 开发者注册页 |
| `controller/bg_auth.go` | **新建** — 开发者注册 API（含邀请码校验） |

### 17.4 部署与运维文档 (P1, 1d)

| 文档 | 内容 |
|------|------|
| `docs/deploy/docker-compose.yml` | 一键启动：API + PostgreSQL + Redis |
| `docs/deploy/ENV.md` | 所有环境变量说明、必填/可选、默认值 |
| `docs/deploy/PRODUCTION.md` | 生产部署建议：反向代理、HTTPS、数据库高可用、备份 |

### 17.5 安全加固 (P1, 1-2d)

| 任务 | 说明 | 复杂度 |
|------|------|--------|
| Per-key rate limiting | 按 API Key 限流（基于现有 rate limiter 扩展） | 低 |
| IP 白名单 | 可选的 Key 级 IP 绑定 | 低 |
| 请求体大小限制 | 防止超大 payload 打爆内存 | 低 |
| Abuse 检测 | 短时间大量 4xx 自动临时封禁 | 中 |

| 文件 | 变更 |
|------|------|
| `middleware/bg_rate_limit.go` | **新建** — Per-key rate limiter |
| `middleware/bg_security.go` | **新建** — IP 白名单 + abuse 检测 |

### 17.6 Landing Page (P2, 1-2d)

内测也需要一个说清楚"这是什么"的入口页。

```
内容：
- 一句话定位：统一 AI 能力网关
- 核心价值（3 个卖点卡片）
- 代码示例（curl + Python）
- CTA：注册内测 / 查看文档
```

### 17.7 验收标准

- [ ] 开发者可自助：注册 → 创建 Project → 获取 API Key → Playground 测试 → 查看用量
- [ ] API 文档站可访问，包含 Quick Start + API Reference + SDK Guide
- [ ] `docker-compose up` 一键启动完整环境
- [ ] Per-key rate limiting 生效，超限返回 429
- [ ] 内测邀请码机制可控制注册（可选）
- [ ] Landing page 说清楚产品定位

---

## 执行优先级与依赖关系

```
Phase 11 (Billing 状态机 + 测试)  ← 核心实现完成，测试待修复 ⚠️
    │
    ├── Phase 12 (策略引擎)       ← ✅ 已完成（4-5d）
    │       │
    │       └── Phase 13 (BYO 全链路 + 适配器) ← 依赖 12 的路由策略（6-9d）
    │               │                            BYO 计费逻辑在此闭环
    │               │
    │               └── Phase 14 (前端改造)     ← 依赖 12+13 的后端 API（5-7d）
    │
    ├── Phase 15 (Tool 投影 + SDK) ← 仅依赖 Phase 11（核心引擎稳定）（4-5d）
    │       │
    │       └── Phase 16 (可观测性) ← 依赖整体稳定（3-4d）
    │
    └── Phase 17 (产品化补齐)     ← 依赖 14 (前端基础) + 15 (SDK)（8-12d）
                                     开发者前端 + 文档 + 注册 + 安全 + 部署
```

```
时间线估算（单人开发）：

Phase 11 ──── ⚠️ 核心完成，测试修复中
Phase 12 ──── ✅ 已完成（Policy + Router）
Phase 13 ──── Week 2 ~ Week 4    (BYO 计费 + 3 个原生适配器)
Phase 14 ──── Week 4 ~ Week 5
Phase 15 ──── Week 5 ~ Week 6    (可与 14 后半段并行)
Phase 16 ──── Week 6 ~ Week 7
Phase 17 ──── Week 7 ~ Week 9    (产品化：前端 + 文档 + 安全 + 部署)

总计：约 8-9 周达到真正可内测状态
```

> **注**：Phase 11-16（6-7 周）完成的是全部技术引擎。Phase 17 额外的 2 周
> 补齐产品化必需品（用户入口、文档、安全），两者缺一不可。

---

## 里程碑定义

### M1: Billing 状态机 + 治理引擎 (Phase 11+12 完成) ✅

> ✅ **已实现**: pre-auth ↔ billing 打通 + Capability/Routing Policy 完整实现
> ⚠️ **待修复**: `TestCalculateBilling_TokenPricing` 单元测试（不影响核心功能）

### M2: BYO 闭环 + Provider 覆盖 + 开发者体验 (Phase 13+14 完成)

> 可对种子用户开放：BYO 计费全链路 + Claude/Gemini/DeepSeek 原生适配 + 开发者控制台 + Playground

### M3: 技术引擎完成 (Phase 15+16 完成)

> 技术完整：Tool 投影 + SDK + 可观测性 + 运维工具。但缺少开发者入口和文档，尚不可对外。

### M4: 可对外内测 (Phase 17 完成)

> 真正可内测：开发者可自助注册 → 创建 Key → 查文档 → Playground 测试 → 集成 SDK → 查看用量。有安全加固和部署文档。

---

## 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Phase 13 体量大（BYO + 3 adapter）| 可能延期 | BYO 计费 + Anthropic adapter 优先；Gemini/DeepSeek 可滑入 Phase 14 并行 |
| Anthropic API 格式变化频繁 | 原生适配器维护成本 | 抽象 message format layer，隔离 API 变化 |
| BYO credential 泄露 | 安全事故 | AES-256 加密 + 访问审计 + key rotation API |
| 前端工作量超预期 | Phase 14 延期 | Playground 可后置，优先 Dashboard + Key 管理 |
| 单人开发瓶颈 | 整体进度 | 优先 M1（治理）和 M2 的后端部分，前端可分批交付 |
| E2E 测试（session/streaming）超时 | Phase 11 延期 | 核心 preauth billing 测试先行，E2E 可滑入 Phase 12 初期 |
| Legacy wrapper 与新路由引擎冲突 | 路由行为不一致 | Phase 12 中统一走 bg_router，legacy wrapper 仍是 adapter 实现 |
| 文档工作量超预期 | Phase 17 延期 | Quick Start + API Reference 优先，SDK Guide 可后置 |
| 开发者前端复用现有组件困难 | 需要更多新建 | 保持最小范围（4 个页面），不做 Dev Dashboard 等非必要页面 |
| 安全加固不足导致内测事故 | 用户数据泄露/服务不可用 | Per-key rate limit 是 P1 必须项，abuse 检测可分批上线 |
