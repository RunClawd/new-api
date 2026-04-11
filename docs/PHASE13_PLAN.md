# Phase 13: BYO 全链路 + 高频原生适配器 (v5 Final)

> **目标**：BYO credential 安全存储 → BYO 计费闭环（支持 crash recovery） → 3 个高频原生适配器交付。
> **总预估**：8–11d（单人），6 个子阶段。
>
> v5 变更（基于 v4 致命逻辑漏洞的修复）：
> 1. 取消 v4 中徒劳的 "fallback 后重新更新 response pricing_snapshot_json"，因为当前 `LookupPricing` 返回的底层基准快照对于 BYO 和 hosted 是一样的，区别只在叠加的费率规则。
> 2. **Complete Attempt Self-Sufficiency (完美 Attempt 闭环)**：把 `PricingSnapshotJSON` 也固化到 `BgResponseAttempt` 上。State machine 终态计费时，所有 metadata (`Pricing`, `BillingSource`, `FeeConfig`) **全部从 winning attempt 读取**，彻底做到 crash-safe，摆脱对 `BgResponse` best-effort update 的依赖。

---

## 设计决策总结

| # | 决策 | 结论 |
|---|------|------|
| 1 | 加密方案 | 独立 `BYO_ENCRYPTION_KEY`，HKDF-SHA256 派生，缺其则 503 |
| 2 | FeeType 范围 | `per_request` + `percentage`；`byo_first` Validate 强制非空 |
| 3 | 存储层面耦合 | Policy 负责费率（`BgRoutingPolicy.RulesJSON`），Credential 负责密钥表 |
| 4 | 适配器优先级 | Gemini + DeepSeek 保底；Anthropic 视 SSE 复杂度而定 |
| 5 | Channel type | 取消新增 type 的计划，复用现有 `ChannelTypeAnthropic/DeepSeek/Gemini` |
| 6 | 路由匹配 | `byo_first` 从查 pattern 改为按 `Provider` 精确匹配 |
| 7 | Override 注入 | 置于 `ResolvedAdapter`（per-adapter），非全局 Request 上，防止泄漏 |
| 8 | 终态计费权威数据 | **`BgResponseAttempt` (Winning Attempt)**。不仅存 `BillingSource`，连 `PricingSnapshot` 一并落盘。 |
| 9 | 预授权结算 | `FinalizeBilling` 返回 `actualQuotaUsed` 计算真实账单，彻底修复当前 pre-existing bug。 |

---

## 核心数据流（BYO Async 完整链路 - v5 版）

```
1. POST /v1/bg/responses {model: "bg.llm.chat.standard", mode: "async"}
   |
2. ResolveRoute -> []ResolvedAdapter
   |- [0] anthropic_native_ch3 {BYO, credential_id=42, feeConfig={percentage,0.10}}
   +- [1] openai_native_ch5   {hosted}
   |
3. 用 [0] metadata 做预授权:
   |  pricing = LookupPricing("bg.llm.chat.standard", "byo")
   |  estimatedQuota = EstimateCost(pricing, input, feeConfig)  <- BYO 只估 platform fee
   |  ReserveQuota(org, estimatedQuota)
   |
4. Insert BgResponse {billing_source="byo", fee_config_json=..., pricing_snapshot_json=...}
   |
5. Fallback loop:
   |  attempt_1: anthropic BYO -> 创建 BgResponseAttempt {billing_source="byo", byo_credential_id=42, fee_config_json=..., pricing_snapshot_json=...}
   |           -> adapter.Invoke(req with CredentialOverride) -> 失败 (429)
   |  attempt_2: openai hosted -> 创建 BgResponseAttempt {billing_source="hosted", pricing_snapshot_json=...}
   |           -> adapter.Invoke(req without override) -> 成功 (accepted)
   |           -> Best-effort update BgResponse {billing_source="hosted"} (for API display only)
   |
6. State machine 终态 (callback/poll):
   |  winning attempt = attempt_2
   |  pricing = attempt_2.PricingSnapshotJSON
   |  feeConfig = attempt_2.FeeConfigJSON -> nil (hosted)
   |  billing_source = attempt_2.BillingSource -> "hosted"
   |
7. FinalizeBilling(hosted, nil) -> hosted 计费 -> returns actualQuotaUsed=3500
   |
8. SettleReservation(estimated=100, actualQuotaUsed=3500) -> 补扣 3400
```

---

## Proposed Changes

### 13.0 前置重构: billing_mode 语义拆分 + actualQuota 结算 (0.5d)

#### 语义拆分（Additive 安全迁移）

| 概念 | 旧字段 | 新字段 | 值域 |
|------|--------|--------|------|
| 结算来源 | `BillingMode` | `BillingSource` | `hosted` / `byo` |
| 计价模式 | `BillingMode` | `PricingMode` | `metered` / `per_call` |

- `BgResponse`/`BgBillingRecord` 均新增这两个 DB 列，保留旧的 `BillingMode` 列双写。
- 在 `BgResponse` 上提供 `GetBillingSource() string` 兜底 fallback 方法，保障 pre-Phase 13 记录运行。
- `relaycommon.PricingSnapshot` 和 `relaycommon.BillingContext` 的结构体也作相应字段重命名。

#### 终态预扣额度差额结算（修复 Pre-existing bug）

```diff
 // service/bg_billing_engine.go
-func FinalizeBilling(responseID string, ...) error
+func FinalizeBilling(responseID string, ...) (actualQuotaUsed int, err error)
     // 内部使用 billingRecord.Amount 换算真实消耗的 Quota

 // service/bg_state_machine.go 终态
-    if err := FinalizeBilling(...); err != nil {
+    actualQuota, billErr := FinalizeBilling(...)
     // ...
-    actualQuota = resp.EstimatedQuota  <- 此逻辑删除
+    SettleReservation(resp.OrgID, resp.EstimatedQuota, actualQuota)
```

> **Warning**: 这不是 BYO 特有的问题。当前 hosted 模式也有同样的 bug：
> `bg_state_machine.go:287-289` 把 `actualQuota = resp.EstimatedQuota`，
> 意味着无论真实账单多少，结算差额永远为 0。

#### 受影响文件列表（13.0）

| 文件 | 变更 |
|------|------|
| `relay/common/canonical.go` | BillingMode -> BillingSource/PricingMode + 新增 CredentialOverride/BYOFeeConfig DTO |
| `model/bg_response.go` | Additive 新增 BillingSource 列 + GetBillingSource() fallback |
| `model/bg_billing.go` | Additive 新增 BillingSource/PricingMode/FeeType |
| `service/bg_billing_engine.go` | PricingMode rename + FinalizeBilling returns actualQuotaUsed |
| `service/bg_state_machine.go` | fallback pricing 用 GetBillingSource() + 消费 actualQuotaUsed |
| `service/bg_orchestrator.go` | BillingMode -> BillingSource 双写 |
| `service/bg_streaming.go` | 同上 |
| `service/bg_preauth_test.go` | BillingMode: "metered" -> PricingMode: "metered" |

---

### 13.1 BYO Credential + ResolvedAdapter + Attempt Metadata (2.5d)

#### Route metadata 持久化设计

> **Important**: 双层落盘原则：
> - `BgResponse`：存首选 adapter 的 metadata（用于预授权计算 + API 展示）
> - `BgResponseAttempt`：存每次尝试的实际 metadata（**终态计费权威来源**）
>
> 终态计费一律从 winning attempt 读取，不从 BgResponse 读。
> 这消除了 crash gap：attempt insert 和 metadata 写入是同一条 SQL。

#### [MODIFY] bg_attempt.go

BgResponseAttempt 新增 4 个字段（additive）：

```go
BillingSource       string `json:"billing_source" gorm:"type:varchar(10);default:'hosted'"`
BYOCredentialID     int64  `json:"byo_credential_id" gorm:"default:0"`
FeeConfigJSON       string `json:"-" gorm:"type:text"`
PricingSnapshotJSON string `json:"-" gorm:"type:text"` // Attempt 级别的基价快照
```

#### [MODIFY] bg_response.go

BgResponse 同样新增字段（用于预授权 + API 展示，非终态计费权威来源）：

```go
BillingSource   string `json:"billing_source" gorm:"type:varchar(10);default:'hosted'"`
BYOCredentialID int64  `json:"byo_credential_id" gorm:"default:0"`
FeeConfigJSON   string `json:"-" gorm:"type:text"`
```

```go
func (r *BgResponse) GetBillingSource() string {
    if r.BillingSource != "" {
        return r.BillingSource
    }
    if r.BillingMode != "" {
        return r.BillingMode
    }
    return "hosted"
}
```

#### [NEW] resolved_adapter.go (`relay/basegate/`)

```go
type ResolvedAdapter struct {
    Adapter            ProviderAdapter
    CredentialOverride *relaycommon.CredentialOverride
    BillingSource      string                         // "hosted" | "byo"
    BYOCredentialID    int64                          // 0 for hosted
    FeeConfig          *relaycommon.BYOFeeConfig
}
```

#### [NEW] bg_byo_credential.go (`model/`)

BYO credential 表定义 + CRUD + 加密。
CapabilitiesJSON 为路由约束，matchCapabilityPattern 检查。

#### [MODIFY] crypto.go (`common/`)

```go
var BYOEncryptionKey string // BYO_ENCRYPTION_KEY env var

func EncryptBYOCredential(plaintext string) (string, error) // HKDF-SHA256 + AES-256-GCM
func DecryptBYOCredential(ciphertext string) (string, error)
func IsBYOEncryptionAvailable() bool { return BYOEncryptionKey != "" }
```

#### [MODIFY] init.go (`common/`)

读取 BYO_ENCRYPTION_KEY 环境变量，不 fallback。

#### [NEW] bg_byo.go (`controller/`)

/api/bg/credentials CRUD。所有 handler 开头检查 IsBYOEncryptionAvailable()，否则 503。

#### [MODIFY] api-router.go

注册 5 个 credential CRUD endpoints。

#### [MODIFY] main.go (`model/`)

migrationModels 添加 BgBYOCredential。

#### [MODIFY] bg_router.go

ResolveRoute 返回 []ResolvedAdapter。
byo_first 按 CapabilityBinding.Provider 匹配。
加 TODO 注释提及 Phase 14 缓存避免热路径查库。

#### [MODIFY] bg_routing_policy.go

BYOFirstRules embed BYOFeeConfig。Validate 强制 FeeType 非空。

#### [MODIFY] bg_orchestrator.go 时序修正

```
ResolveRoute -> resolvedAdapters
-> 用 [0] metadata 做预授权
-> Insert BgResponse（首选 metadata）
-> Fallback loop:
     -> attempt.Insert() 同时写入 BillingSource/FeeConfigJSON/PricingSnapshotJSON
     -> shallowCopyWithOverride(req, resolved.CredentialOverride)
     -> Invoke
     -> 成功后 best-effort 更新 BgResponse（仅 API 展示）
     -> 更新 BYO credential LastUsedAt
```

#### [MODIFY] bg_streaming.go

同样的时序调整。

#### [MODIFY] bg_session_manager.go (stretch)

消费 []ResolvedAdapter。

#### [MODIFY] bg_sessions.go

补 BillingContext{BillingSource: "hosted"}。

#### [MODIFY] bg_router_test.go

更新 byo_adapter_pattern -> provider 匹配格式。

---

### 13.2 BYO 计费逻辑闭环 (1.5d)

#### [MODIFY] bg_state_machine.go

终态计费从 winning attempt 读取所有 metadata：

```go
// 从 Attempt 读取一切
pricing = attempt.PricingSnapshotJSON (解析)
feeConfig = attempt.FeeConfigJSON (解析)
billingSource = attempt.BillingSource
// 如果旧数据缺失 attempt 快照，fallback 回 BgResponse
```

FinalizeBilling 接受 feeConfig 和 billingSource。
actualQuota 用于 SettleReservation。

#### [MODIFY] bg_billing_engine.go

FinalizeBilling 完整签名：

```go
func FinalizeBilling(responseID string, orgID, projectID int, modelName, provider string,
    rawUsage *relaycommon.ProviderUsage, pricing *relaycommon.PricingSnapshot,
    billingSource string, feeConfig *relaycommon.BYOFeeConfig) (actualQuotaUsed int, err error)
```

BYO 分支：Amount = CalculateBYOFee, ProviderCost = 0, PlatformMargin = Amount

#### [MODIFY] bg_preauth.go

EstimateCost 新增 feeConfig 参数。BYO 时只估 platform fee。

#### [NEW] bg_billing_engine_byo_test.go

- TestCalculateBYOFee_PerRequest / _Percentage
- TestFinalizeBYOBilling
- TestFinalizeBilling_Returns_ActualQuota
- TestSettleReservation_BYOFallbackToHosted
- TestStateMachineTerminal_BYO_FromAttempt

---

### 13.3 Anthropic Claude 原生适配器 (2d)

#### [NEW] anthropic_llm_adapter.go + _test.go

模型映射：bg.llm.chat.fast -> claude-haiku-4-5, standard -> claude-sonnet-4-5, pro -> claude-opus-4, reasoning.pro -> claude-opus-4-6

Invoke: POST /v1/messages
Stream: SSE 5 种事件类型 + thinking content block
CredentialOverride 支持

可从 legacy 复用：stopReasonClaude2OpenAI(), SSE switch-case 框架
需重写：RequestOpenAI2ClaudeMessage()（输入格式不同）

---

### 13.4 Google Gemini 原生适配器 (1.5d)

#### [NEW] gemini_llm_adapter.go + _test.go

constant.ChannelTypeGemini (= 24)
Phase 13 Scope: AI Studio only (API Key auth)
Invoke: POST generateContent?key={apiKey}
Stream: POST streamGenerateContent?alt=sse&key={apiKey}

---

### 13.5 DeepSeek 原生适配器 (1d)

#### [NEW] deepseek_llm_adapter.go + _test.go

baseURL = https://api.deepseek.com
大部分复用 OpenAI adapter 逻辑
差异：reasoning_content 字段映射

---

### 13.6 适配器注册 + 联合测试 (1d)

#### [MODIFY] bg_register.go

RegisterNativeAdapters() 新增 3 段：ChannelTypeAnthropic / ChannelTypeGemini / ChannelTypeDeepSeek

#### [MODIFY] openai_llm_adapter.go

CredentialOverride 支持（4 行）。

#### [NEW] bg_byo_e2e_test.go

完整 E2E：
1. Create BYO credential
2. Create byo_first routing policy
3. Sync BYO -> billing_source=byo, provider_cost=0
4. BYO fallback -> hosted: billing_source=hosted
5. Async BYO -> state machine 从 attempt 读 FeeConfigJSON -> 正确计费
6. 预授权结算：BYO 估 100 -> hosted 实际 3500 -> SettleReservation 补扣 3400
7. Crash recovery：attempt 有 metadata -> 即使 BgResponse 未更新 -> 终态仍正确

---

## 交付物清单 (共计 33 项)

| # | 文件 | 类型 | 子阶段 |
|---|------|------|--------|
| 1 | `relay/common/canonical.go` | 修改 | 13.0 + 13.1 |
| 2 | `model/bg_response.go` | 修改 | 13.0 + 13.1 |
| 3 | `model/bg_billing.go` | 修改 | 13.0 + 13.2 |
| 4 | `model/bg_attempt.go` | 修改 | 13.1 |
| 5 | `service/bg_billing_engine.go` | 修改 | 13.0 + 13.2 |
| 6 | `service/bg_state_machine.go` | 修改 | 13.0 + 13.1 + 13.2 |
| 7 | `service/bg_orchestrator.go` | 修改 | 13.0 + 13.1 + 13.2 |
| 8 | `service/bg_streaming.go` | 修改 | 13.0 + 13.1 + 13.2 |
| 9 | `service/bg_session_manager.go` | 修改 | 13.1 (stretch) |
| 10 | `controller/bg_sessions.go` | 修改 | 13.1 |
| 11 | `service/bg_preauth.go` | 修改 | 13.2 |
| 12 | `service/bg_preauth_test.go` | 修改 | 13.0 |
| 13 | `model/bg_byo_credential.go` | **新建** | 13.1 |
| 14 | `common/crypto.go` | 修改 | 13.1 |
| 15 | `common/init.go` | 修改 | 13.1 |
| 16 | `controller/bg_byo.go` | **新建** | 13.1 |
| 17 | `router/api-router.go` | 修改 | 13.1 |
| 18 | `model/main.go` | 修改 | 13.1 |
| 19 | `relay/basegate/resolved_adapter.go` | **新建** | 13.1 |
| 20 | `service/bg_router.go` | 修改 | 13.1 |
| 21 | `model/bg_routing_policy.go` | 修改 | 13.1 |
| 22 | `service/bg_router_test.go` | 修改 | 13.1 |
| 23 | `service/bg_billing_engine_byo_test.go` | **新建** | 13.2 |
| 24 | `relay/basegate/adapters/anthropic_llm_adapter.go` | **新建** | 13.3 |
| 25 | `relay/basegate/adapters/anthropic_llm_adapter_test.go` | **新建** | 13.3 |
| 26 | `relay/basegate/adapters/gemini_llm_adapter.go` | **新建** | 13.4 |
| 27 | `relay/basegate/adapters/gemini_llm_adapter_test.go` | **新建** | 13.4 |
| 28 | `relay/basegate/adapters/deepseek_llm_adapter.go` | **新建** | 13.5 |
| 29 | `relay/basegate/adapters/deepseek_llm_adapter_test.go` | **新建** | 13.5 |
| 30 | `relay/bg_register.go` | 修改 | 13.6 |
| 31 | `relay/basegate/adapters/openai_llm_adapter.go` | 修改 | 13.6 |
| 32 | `controller/bg_byo_e2e_test.go` | **新建** | 13.6 |

---

## Verification Plan

### Automated Tests

```bash
# 13.0 -- 语义拆分 + actualQuota 回归
go test ./service/ -run TestFinalizeBilling -v       # 验证返回 actualQuotaUsed
go test ./service/ -run TestSettleReservation -v     # 验证差额结算
go test ./service/ -run TestPreauth -v

# 13.1 -- BYO Credential + ResolvedAdapter + Attempt metadata
go test ./model/ -run TestBgBYOCredential -v -race
go test ./common/ -run TestBYOEncrypt -v
go test ./service/ -run TestResolveRoute_BYOFirst -v -race

# 13.2 -- BYO Billing + State machine rehydration
go test ./service/ -run TestBYO -v -race
go test ./service/ -run TestStateMachineTerminal_BYO -v   # 从 attempt 读取

# 13.3-13.5 -- Adapters
go test ./relay/basegate/adapters/ -v

# 13.6 -- E2E + Full regression
go test ./controller/ -run TestBYOE2E -v
go test ./... -race -count=1
```

### Manual Verification

1. **加密**: credential 创建 -> DB encrypted_value 不可读 -> API masked_value
2. **路由**: byo_first + credential -> BYO adapter（日志 adapter + credential ID）
3. **终态计费（Async）**: state machine 从 attempt 读 PricingSnapshot/BillingSource/FeeConfigJSON -> 正确计费
4. **预授权差额**: BYO 估 100 -> hosted 实际 3500 -> 补扣 3400（查 quota 变化）
5. **Crash recovery**: attempt 有 metadata -> 即使 BgResponse 未更新 -> 终态仍正确
6. **旧数据兼容**: pre-Phase 13 response/attempt/billing 空值 fallback -> hosted/metered
7. **缺失 BYO_ENCRYPTION_KEY**: BYO CRUD -> 503

---

## 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| FinalizeBilling 返回值变更影响调用点 | 回归 | 旧调用点 `_, err := FinalizeBilling(...)` 即可 |
| BgResponseAttempt 新增 4 列 | 迁移 | additive，AutoMigrate 自动加列 |
| Anthropic SSE 复杂度 | 13.3 超 2d | Gemini + DeepSeek 保底 |
| byo_first per-request DB | 延迟 | TODO 标注，Phase 14 缓存 |
| Additive 旧列清理 | 列冗余 | Phase 14 首 task 删旧列 |
