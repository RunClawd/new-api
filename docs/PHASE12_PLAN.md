# Phase 12: Capability Policy & Routing Policy

> **目标**：实现 Schema 九中定义的能力策略和路由策略，让不同租户可以有不同的权限和调度行为。  
> **预估工时**：Day 0（前置 micro-PR, 1-2h）+ Day 1-5（主体, 4-5 天）  
> **前置依赖**：Phase 11（Billing 状态机 + 测试加固）已完成

---

## User Review Required

> [!IMPORTANT]
> **路由策略接入方式**：Phase 12 将用 `bg_router.ResolveRoute()` 取代 `basegate.LookupAdapters()` 作为 orchestrator 的主入口。现有的 `LookupAdapters()` 降级为 `bg_router` 的内部 fallback（即无 policy 配置时的默认行为）。这是向后兼容的——无 policy 时行为完全不变。

> [!WARNING]
> **Legacy wrapper 兼容**：Legacy wrapper 的请求同样会被 `bg_router` 管理。需要确保路由策略 `RulesJSON` 中的适配器匹配规则（如 `adapter_name`、`primary`、`weights` 键或 `byo_adapter_pattern`）能匹配 legacy wrapper 的 name（格式：`legacy_task_<platform>`，如 `legacy_task_suno`、`legacy_task_kling`）。参见 [legacy_wrapper.go:24](file:///Users/xiongtengyan/code/basegate/new-api/relay/basegate/legacy_wrapper.go#L24) 和 [bg_register.go:28](file:///Users/xiongtengyan/code/basegate/new-api/relay/bg_register.go#L28)。

---

## 设计分析

### 现状

当前请求分发路径：

```
PostResponses controller (bg_responses.go:16-131):
  → parse request + build CanonicalRequest
  → switch mode:
      sync:   service.DispatchSync(req)
      async:  service.DispatchAsync(req)
      stream: service.DispatchStream(req, c)
  → error handling (L103-121): == ErrIdempotencyConflict / == ErrInsufficientQuota

DispatchSync (bg_orchestrator.go:54-191):
  → basegate.LookupAdapters(req.Model)           // 从 capabilityMap 按名称查找
    → weightedRandomSort([]weightedAdapter)       // 按 Weight 加权随机排序
  → for adapter in adapters:                      // Fallback loop
       adapter.Validate(req) → adapter.Invoke(req)
```

**问题**：
1. **无访问控制**：任何 org/project/key 都能调用任何已注册的 capability
2. **路由策略不可配置**：只有全局 `CapabilityBinding.Weight` 一种策略，无法 per-tenant 定制
3. **Stream 错误路径独立**：`bg_responses.go:84-98` 的 stream error handler 与 sync/async 的 error block (L103-121) 是分离的，新增错误类型需要两处都改
4. **Error 比较风格不一致**：L107-112 用 `==` 比较 sentinel error，不支持 wrapped error

### 目标架构

```
PostResponses controller:
  → parse request + build CanonicalRequest
  → EvaluateCapabilityAccess(orgID, projectID, apiKeyID, model)   ← 一处检查, 403 early return
  → switch mode:
      sync:   DispatchSync(req)      // 内部只做 ResolveRoute + invoke
      async:  DispatchAsync(req)
      stream: DispatchStream(req, c)
  → writeBGError(c, err)             ← 统一错误映射（前置 micro-PR 提供）

DispatchSync:
  → ResolveRoute(orgID, projectID, apiKeyID, capability)
    → 有 routing policy → ListAdaptersUnordered() + 策略排序
    → 无 routing policy → LookupAdapters() 原逻辑（含 weighted shuffle）
  → for adapter in adapters: ...
```

**关键决策：Capability check 在 controller 层，Route resolution 在 service 层**

| 职责 | 层级 | 理由 |
|------|------|------|
| `EvaluateCapabilityAccess` | Controller | 纯读无副作用；一处覆盖 sync/async/stream 三路径；避免 stream 特殊 error handling 问题 |
| `ResolveRoute` | Service (inside Dispatch*) | 需要 adapter 列表来执行后续 invoke/fallback 逻辑；与 orchestrator 紧密耦合 |

---

## Proposed Changes

### Component 1: Data Model

#### [NEW] bg_capability_policy.go (`model/bg_capability_policy.go`)

Capability Policy 表定义 + CRUD + 校验方法。

```go
type BgCapabilityPolicy struct {
    ID                int64  `gorm:"primaryKey;autoIncrement"`
    Scope             string `gorm:"type:varchar(20);not null;index:idx_cap_pol_scope"`       // platform | org | project | key
    ScopeID           int    `gorm:"not null;default:0;index:idx_cap_pol_scope"`               // 0 for platform scope
    CapabilityPattern string `gorm:"type:varchar(191);not null;index:idx_cap_pol_pattern"`     // "bg.llm.*" or "bg.video.generate.standard"
    Action            string `gorm:"type:varchar(10);not null;default:'allow'"`                 // allow | deny
    Enforced          bool   `gorm:"not null;default:false"`                                    // true = 不可被下级 scope 覆盖
    MaxConcurrency    int    `gorm:"not null;default:0"`                                        // reserved, Validate() enforces 0
    Priority          int    `gorm:"not null;default:0"`                                        // higher = evaluated first within same scope
    Description       string `gorm:"type:varchar(500)"`
    Status            string `gorm:"type:varchar(20);not null;default:'active'"`                // active | disabled
    CreatedAt         int64  `gorm:"autoCreateTime"`
    UpdatedAt         int64  `gorm:"autoUpdateTime"`
}
```

**`Enforced` 字段语义**：
- 仅对 `platform` scope 有意义
- `Validate()` 直接**拒绝** `scope != "platform" && enforced == true`（fail early，不做 silent ignore）
- 当 `platform deny + enforced=true` 时，即使 org/project/key 有 allow 也不生效

**Model 层校验**（防止其他入口绕过 controller 校验）：

```go
func (p *BgCapabilityPolicy) Validate() error {
    validScopes := map[string]bool{"platform": true, "org": true, "project": true, "key": true}
    if !validScopes[p.Scope] {
        return fmt.Errorf("invalid scope: %s", p.Scope)
    }
    if p.Scope == "platform" && p.ScopeID != 0 {
        return fmt.Errorf("platform scope must have scope_id=0")
    }
    if !strings.HasPrefix(p.CapabilityPattern, "bg.") {
        return fmt.Errorf("capability_pattern must start with 'bg.'")
    }
    // Wildcard '*' must be the last segment — reject patterns like "bg.*.chat"
    parts := strings.Split(p.CapabilityPattern, ".")
    for i, part := range parts {
        if part == "*" && i != len(parts)-1 {
            return fmt.Errorf("wildcard '*' must be the last segment in capability_pattern")
        }
    }
    if p.Action != "allow" && p.Action != "deny" {
        return fmt.Errorf("action must be 'allow' or 'deny'")
    }
    if p.Enforced && p.Scope != "platform" {
        return fmt.Errorf("enforced=true is only allowed on platform scope")
    }
    if p.MaxConcurrency != 0 {
        return fmt.Errorf("max_concurrency is reserved for future use and must be 0")
    }
    return nil
}
```

**CRUD 方法**：
- `CreateBgCapabilityPolicy(policy *BgCapabilityPolicy) error`
- `GetActiveBgCapabilityPolicies() ([]BgCapabilityPolicy, error)` — 供缓存使用
- `UpdateBgCapabilityPolicy(policy *BgCapabilityPolicy) error`
- `DeleteBgCapabilityPolicy(id int64) error`
- `ListBgCapabilityPolicies(scope string, scopeID int, offset, limit int) ([]BgCapabilityPolicy, int64, error)`

---

#### [NEW] bg_routing_policy.go (`model/bg_routing_policy.go`)

Routing Policy 表定义 + CRUD + 校验方法。

```go
type BgRoutingPolicy struct {
    ID                int64  `gorm:"primaryKey;autoIncrement"`
    Scope             string `gorm:"type:varchar(20);not null;index:idx_rt_pol_scope"`
    ScopeID           int    `gorm:"not null;default:0;index:idx_rt_pol_scope"`
    CapabilityPattern string `gorm:"type:varchar(191);not null;index:idx_rt_pol_pattern"`
    Strategy          string `gorm:"type:varchar(30);not null;default:'weighted'"`              // weighted | fixed | primary_backup | byo_first
    RulesJSON         string `gorm:"type:text"`
    Priority          int    `gorm:"not null;default:0"`
    Description       string `gorm:"type:varchar(500)"`
    Status            string `gorm:"type:varchar(20);not null;default:'active'"`
    CreatedAt         int64  `gorm:"autoCreateTime"`
    UpdatedAt         int64  `gorm:"autoUpdateTime"`
}
```

**RulesJSON 结构示例**：

```json
// Strategy: fixed
{"adapter_name": "openai-llm-ch1"}

// Strategy: primary_backup
{"primary": "openai-llm-ch1", "fallback": ["anthropic-llm-ch2", "deepseek-llm-ch3"]}

// Strategy: weighted
{"weights": {"openai-llm-ch1": 70, "anthropic-llm-ch2": 30}}

// Strategy: byo_first
{"byo_adapter_pattern": "*byo*"} // Fallback is implicitly basegate.LookupAdapters()
```

**Model 层校验**：

```go
func (p *BgRoutingPolicy) Validate() error {
    // 1. scope/scopeID/capability_pattern 规则与 capability policy 相同
    //    (包括 `*` 必须在 capability_pattern 的最后一段)
    // 2. strategy 必须是 fixed/weighted/primary_backup/byo_first
    // 3. rules_json 必须是合法 JSON 且必须符合 strategy 结构规则：
    //    - fixed: adapter_name 不能为空
    //    - primary_backup: primary 不能为空
    //    - weighted: weights map 不能为空，且所有 value 必须 > 0
    //    - byo_first: byo_adapter_pattern 不能为空
}
```

---

#### [MODIFY] main.go (`model/main.go`)

在 `migrateDB()` 的 `migrationModels` 列表中添加两个新表（L303 之后）。

```diff
 {"BgProject", &BgProject{}},
+{"BgCapabilityPolicy", &BgCapabilityPolicy{}},
+{"BgRoutingPolicy", &BgRoutingPolicy{}},
```

---

### Component 2: Adapter Registry 扩展

#### [MODIFY] adapter_registry.go (`relay/basegate/adapter_registry.go`)

新增 `ListAdaptersUnordered`，供 routing policy 引擎使用（不做 weighted shuffle）。
**Copy 在锁内完成**，防止 `RegisterAdapter` 的 `append` 替换 slice header 导致竞态。

```go
// ListAdaptersUnordered returns all registered adapters for a capability without any sorting.
// Used by the routing policy engine (bg_router.go) which applies its own ordering strategy.
// For the original weighted-random behavior, use LookupAdapters().
//
// Phase 13: consider returning weight/provider metadata if BYO fallback needs original weights.
func ListAdaptersUnordered(capabilityName string) []ProviderAdapter {
    adapterRegistryMu.RLock()
    weighted := capabilityMap[capabilityName]
    // Copy under lock — RegisterAdapter may replace the slice via append
    result := make([]ProviderAdapter, len(weighted))
    for i, wa := range weighted {
        result[i] = wa.Adapter
    }
    adapterRegistryMu.RUnlock()
    return result
}
```

`LookupAdapters` 保持不变（仅在无 routing policy 时作为 fallback 使用）。

> **Note**: 当前返回 `[]ProviderAdapter`（不含 Weight/Provider 元数据）对 Phase 12 的四种策略足够：
> 所有策略通过 `adapter.Name()` 匹配即可。如果 Phase 13 BYO fallback 需要原始 weight，
> 再扩展返回类型为 `[]RouteCandidate{Adapter, Name, Weight, Provider}`。

---

### Component 3: Policy Evaluation Engine

#### [NEW] bg_policy_engine.go (`service/bg_policy_engine.go`)

Capability Policy 评估引擎。

```go
// ErrCapabilityDenied is a sentinel error for capability policy denial.
// Use errors.Is(err, ErrCapabilityDenied) in controller to map to HTTP 403.
var ErrCapabilityDenied = fmt.Errorf("capability_denied")
```

核心 API：

```go
// EvaluateCapabilityAccess checks whether the given identity is allowed to use a capability.
// Called in controller layer BEFORE mode dispatch (one check covers sync/async/stream).
// Returns (allowed bool, reason string, err error).
//
// Evaluation algorithm:
//
//   Phase 0 — Initialization check:
//     If cacheInitialized.Load() is false, return (false, "", errorEngineNotReady).
//     This prevents fail-open behavior if the DB is unavailable during early startup.
//
//   Phase A — Enforced platform deny (short-circuit):
//     Scan all platform-level policies with enforced=true and action=deny.
//     If any matches the capability: return deny immediately. No override possible.
//
//   Phase B — Hierarchical evaluation (from highest to lowest priority):
//     1. Key-level policies (scope="key", scopeID=apiKeyID)
//     2. Project-level policies (scope="project", scopeID=projectID)
//     3. Org-level policies (scope="org", scopeID=orgID)
//     4. Platform-level policies (scope="platform", scopeID=0, enforced=false)
//
//   Within the same scope level:
//     - Higher Priority value takes precedence
//     - More specific pattern > wildcard pattern (specificity score)
//     - deny wins over allow at same priority + specificity
//
//   If no policy matches in any scope: default ALLOW (backward compatible).
func EvaluateCapabilityAccess(orgID, projectID, apiKeyID int, capabilityName string) (bool, string, error)
```

**Glob 匹配规则**：

> **Note**: BaseGate 使用非标准 glob 语义：`*` 匹配剩余所有段（跨 `.` 分隔符）。
> 这不是 `filepath.Match` 的行为。采用自定义 `matchCapabilityPattern()` 实现。

```go
// matchCapabilityPattern matches a capability name against a pattern.
// Pattern rules:
//   - Exact match: "bg.llm.chat.standard" matches "bg.llm.chat.standard"
//   - Wildcard suffix: "bg.llm.*" matches "bg.llm.chat.standard", "bg.llm.reasoning.pro", etc.
//   - The wildcard "*" must appear as the last segment and matches ALL remaining segments.
//   - This is NOT standard glob behavior (standard * does not cross . boundaries).
//
// Implementation: split both by ".", compare segment by segment.
// When pattern segment is "*", return match immediately (all remaining segments match).
func matchCapabilityPattern(pattern, capabilityName string) bool {
    patternParts := strings.Split(pattern, ".")
    nameParts := strings.Split(capabilityName, ".")

    for i, pp := range patternParts {
        if pp == "*" {
            return true // wildcard matches all remaining segments
        }
        if i >= len(nameParts) {
            return false // pattern has more non-wildcard segments than name
        }
        if pp != nameParts[i] {
            return false // segment mismatch
        }
    }
    return len(patternParts) == len(nameParts)
}

// capabilitySpecificity returns the number of non-wildcard segments.
// Higher = more specific. "bg.llm.chat.standard" → 4, "bg.llm.*" → 2, "bg.*" → 1
func capabilitySpecificity(pattern string) int { ... }
```

**缓存策略**（简化设计：全量加载 + CRUD 同步 reload + 60s 兜底）：

```go
var (
    policyMu           sync.RWMutex
    capabilityPolicies []model.BgCapabilityPolicy
    routingPolicies    []model.BgRoutingPolicy
    cacheInitialized   atomic.Bool // Prevents fail-open if initial DB load fails
)

// RefreshPolicyCache does a full reload from DB.
// On failure: retains old cache + logs error (never crashes, never falls back to allow-all).
// Called: startup, after CRUD (sync), 60s ticker (fallback).
func RefreshPolicyCache() error {
    capPolicies, err := model.GetActiveBgCapabilityPolicies()
    if err != nil {
        return fmt.Errorf("failed to load capability policies: %w", err)
    }
    rtPolicies, err := model.GetActiveBgRoutingPolicies()
    if err != nil {
        return fmt.Errorf("failed to load routing policies: %w", err)
    }

    // Sort by scope level → priority → specificity
    // ... (sorting logic)

    policyMu.Lock()
    capabilityPolicies = capPolicies
    routingPolicies = rtPolicies
    policyMu.Unlock()
    cacheInitialized.Store(true)
    return nil
}

// InvalidatePolicyCache is called after any CRUD operation — synchronous reload.
// Returns an error if the reload fails. Admin API should return cache_sync_failed.
func InvalidatePolicyCache() error {
    if err := RefreshPolicyCache(); err != nil {
        common.SysError("policy cache refresh failed: " + err.Error())
        return err
    }
    return nil
}

// StartPolicyCacheRefresher starts a background goroutine that periodically
// refreshes the policy cache as a fallback (covers distributed deployments).
// Called during application startup alongside StartReconciliationWorker.
func StartPolicyCacheRefresher(interval time.Duration) {
    // Synchronous initial load to ensure cache is hot before handling requests
    if err := RefreshPolicyCache(); err != nil {
        common.SysError("policy_cache: initial load failed: " + err.Error())
    }

    go func() {
        common.SysLog(fmt.Sprintf("policy_cache: refresher started (interval: %s)", interval))
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
            if err := RefreshPolicyCache(); err != nil {
                common.SysError("policy_cache: periodic refresh failed: " + err.Error())
            }
        }
    }()
}
```

> **Note**: 刻意不使用 `MAX(updated_at)` 短路或 `atomic.Int64`。对 <100 条 policy 来说，
> 每 60s 全量 reload 的开销 <1ms，简化设计优先于微优化。
> 如果 policy 规模增长到需要短路优化，再加 `MAX(updated_at)` 检查。

**启动点**：在 `main.go` 的 startup sequence 中，与 `StartReconciliationWorker` 同级调用：
```go
service.StartPolicyCacheRefresher(60 * time.Second)
```

---

#### [NEW] bg_policy_engine_test.go (`service/bg_policy_engine_test.go`)

**Policy 评估测试**：

| # | 场景 | 预期结果 |
|---|------|---------|
| 1 | 无 policy + 任意 capability | allow (默认) |
| 2 | platform deny `bg.sandbox.*` + org 无 policy | deny |
| 3 | platform deny `bg.sandbox.*` (enforced=false) + org allow `bg.sandbox.*` | allow (org > platform) |
| 4 | platform deny `bg.sandbox.*` (enforced=true) + org allow `bg.sandbox.*` | **deny** (enforced 不可覆盖) |
| 5 | org deny `bg.sandbox.*` + project allow `bg.sandbox.session.standard` | allow (project > org, 且更精确) |
| 6 | key deny `bg.llm.*` | deny (key 最高) |
| 7 | platform allow `bg.*` + platform deny `bg.sandbox.*` | `bg.llm.chat.standard` → allow, `bg.sandbox.session.standard` → deny |
| 8 | 同级 allow + deny 同 priority + 同 specificity | deny (同级 deny 优先) |
| 9 | disabled policy 不参与评估 | allow |
| 10 | `matchCapabilityPattern` 单元测试 | `bg.llm.*` 匹配 `bg.llm.chat.standard`；不匹配 `bg.video.generate.standard` |
| 11 | `capabilitySpecificity` 单元测试 | 4段 > 3段 > 2段 > 1段 |
| 12 | Validate() 拒绝 `scope="org" + enforced=true` | error |
| 13 | Validate() 拒绝 `MaxConcurrency != 0` | error（reserved 字段） |

**缓存行为测试**：

| # | 场景 | 预期结果 |
|---|------|---------|
| 14 | policy delete 后 `InvalidatePolicyCache()` | 缓存立即生效（已删 policy 不再匹配） |
| 15 | `RefreshPolicyCache()` DB 查询失败 | 返回 error，保留旧缓存，不 crash |
| 16 | `cacheInitialized=false` 时调用 `EvaluateCapabilityAccess` | 返回 error（非 allow、非 deny），防止 fail-open 导致越权 |

---

### Component 4: Routing Policy Engine

#### [NEW] bg_router.go (`service/bg_router.go`)

策略感知的路由引擎。

```go
// ResolveRoute returns an ordered list of ProviderAdapters for a given capability,
// applying any matching routing policies for the tenant.
//
// Resolution:
//   1. Find the most specific matching routing policy (key > project > org > platform)
//   2. If found: get unordered adapters via ListAdaptersUnordered(), apply strategy
//   3. If not found: fallback to basegate.LookupAdapters() (original weighted shuffle)
func ResolveRoute(orgID, projectID, apiKeyID int, capabilityName string) ([]basegate.ProviderAdapter, error)

// resolveRouteWithRand is the internal implementation with injectable randomness for testing.
func resolveRouteWithRand(rng *rand.Rand, orgID, projectID, apiKeyID int, capabilityName string) ([]basegate.ProviderAdapter, error)
```

**关键设计**：
- 有 routing policy → `basegate.ListAdaptersUnordered()` + 策略排序（绕开无用 shuffle）
- 无 routing policy → `basegate.LookupAdapters()`（保留原有行为，完全向后兼容）

**策略实现**：

```go
func applyFixedStrategy(rules FixedRules, allAdapters []basegate.ProviderAdapter) ([]basegate.ProviderAdapter, error)
func applyWeightedStrategy(rng *rand.Rand, rules WeightedRules, allAdapters []basegate.ProviderAdapter) []basegate.ProviderAdapter
func applyPrimaryBackupStrategy(rules PrimaryBackupRules, allAdapters []basegate.ProviderAdapter) ([]basegate.ProviderAdapter, error)
func applyBYOFirstStrategy(rng *rand.Rand, rules BYOFirstRules, allAdapters []basegate.ProviderAdapter) []basegate.ProviderAdapter
```

**Rules 类型**：

```go
type FixedRules struct {
    AdapterName string `json:"adapter_name"`
}
type WeightedRules struct {
    Weights map[string]int `json:"weights"`
}
type PrimaryBackupRules struct {
    Primary  string   `json:"primary"`
    Fallback []string `json:"fallback"`
}
type BYOFirstRules struct {
    BYOAdapterPattern string `json:"byo_adapter_pattern"`
}
```

---

#### [NEW] bg_router_test.go (`service/bg_router_test.go`)

所有含随机性的测试使用 `resolveRouteWithRand(rand.New(rand.NewSource(fixedSeed)), ...)` 确保确定性。

| # | 场景 | 预期结果 |
|---|------|---------|
| 1 | 无 routing policy | 等价于 `LookupAdapters()` 原行为 |
| 2 | fixed 策略指定 adapter-A | 返回 [adapter-A] |
| 3 | fixed 指定不存在的 adapter | error（明确配置错误） |
| 4 | weighted {A:70, B:30}，固定 seed | 确定性排序结果 |
| 5 | primary_backup {A, [B,C]} | 返回 [A, B, C] |
| 6 | org policy + project 不同 policy | project policy 生效 |
| 7 | byo_first（无 BYO adapter） | fallback 到 `LookupAdapters()` 以保留权重 |
| 8 | ListAdaptersUnordered 返回完整列表 | 与注册顺序一致，未 shuffle |
| 9 | legacy wrapper 名称匹配（`legacy_task_suno`, `legacy_task_kling`） | fixed/weighted 策略正确匹配 |

---

### Component 5: 前置 micro-PR — 统一错误映射 + 测试基建修正

> **Important**: 在 Phase 12 正式开始之前，先提交一个独立的 micro-PR（1-2h），完成两件事：
> 1. 提取统一的 `writeBGError` 函数（已有 tech debt）
> 2. **修正 controller 测试 helper 的 context key**（pre-existing bug）
>
> 独立提交避免 Phase 12 PR review 同时涉及新功能和基建修复。

#### [NEW] bg_error.go (`controller/bg_error.go`)

统一错误映射函数，所有 BaseGate controller（responses / sessions）统一使用：

> **Warning**: **`writeBGError` 仅用于"响应尚未开始写出"的路径。**
> Stream 分支中 `DispatchStream` 返回 error 后，必须保留 `!c.Writer.Written()` 守卫——
> 一旦 SSE header/body 已开始写出，再调用 `c.JSON()` 会破坏响应。
> Stream 错误后只记录日志，不追加 JSON body。

```go
// writeBGError maps service-layer sentinel errors to structured HTTP error responses.
// Covers all BaseGate sentinel errors across responses and sessions.
//
// IMPORTANT: This function calls c.JSON(), which is NOT safe after SSE headers
// have been written. For stream paths, callers MUST check !c.Writer.Written()
// before calling this function. If headers are already sent, log the error only.
func writeBGError(c *gin.Context, err error) {
    statusCode := http.StatusInternalServerError
    errCode := "internal_error"
    errType := "api_error"

    switch {
    case errors.Is(err, service.ErrCapabilityDenied):
        statusCode = http.StatusForbidden
        errCode = "capability_denied"
        errType = "permission_error"
    case errors.Is(err, service.ErrIdempotencyConflict):
        statusCode = http.StatusConflict
        errCode = "idempotency_mismatch"
        errType = "invalid_request_error"
    case errors.Is(err, service.ErrInsufficientQuota):
        statusCode = http.StatusPaymentRequired
        errCode = "insufficient_quota"
        errType = "invalid_request_error"
    case errors.Is(err, service.ErrSessionValidation):
        statusCode = http.StatusBadRequest
        errCode = "invalid_request"
        errType = "invalid_request_error"
    case errors.Is(err, service.ErrSessionNotFound):
        statusCode = http.StatusNotFound
        errCode = "not_found"
        errType = "invalid_request_error"
    case errors.Is(err, service.ErrSessionBusy):
        statusCode = http.StatusConflict
        errCode = "conflict"
        errType = "api_error"
    case errors.Is(err, service.ErrSessionTerminal):
        statusCode = http.StatusBadRequest
        errCode = "invalid_request"
        errType = "invalid_request_error"
    case errors.Is(err, service.ErrSessionAdapter):
        statusCode = http.StatusBadGateway
        errCode = "bad_gateway"
        errType = "api_error"
    }

    c.JSON(statusCode, gin.H{
        "error": gin.H{
            "code":    errCode,
            "type":    errType,
            "message": err.Error(),
        },
    })
}
```

micro-PR 同时重构 `bg_responses.go` 和 `bg_sessions.go` 的 error handler 为 `writeBGError(c, err)` 调用。
Stream 分支保留原有的 `!c.Writer.Written()` 守卫模式。

#### [MODIFY] bg_responses_test.go (`controller/bg_responses_test.go`)

**修正 context key 不匹配问题**（pre-existing bug）：

当前 controller 读取 `c.GetInt("id")` (orgID) 和 `c.GetInt("token_id")` (apiKeyID)，
但测试 helper `newBgTestContext` 设置的是 `ctx.Set("org_id", 1)` 和 `ctx.Set("api_key_id", 10)`。
`GetInt` 查找不到时返回 0，导致所有 policy 测试的 orgID/apiKeyID 都是 0，scope 匹配全部失效。

```diff
 // Set default auth context
-ctx.Set("org_id", 1)
-ctx.Set("project_id", 1)
-ctx.Set("api_key_id", 10)
+ctx.Set("id", 1)        // matches c.GetInt("id") in bg_responses.go:45
+ctx.Set("token_id", 10) // matches c.GetInt("token_id") in bg_responses.go:47
 ctx.Set("end_user_id", "user_test")
```

> **Caution**: **不修这个 bug，Phase 12 所有按 org/project/key scope 生效的 policy 测试都不可信**。
> 这是一个 pre-existing 的测试基础设施问题，必须在 Phase 12 之前修好。
> 注意 `projectID` 来自 `c.GetHeader("X-Project-Id")`（非 context），测试中通过 request header 设置。

---

### Component 6: Controller 层集成（Phase 12 本体）

#### [MODIFY] bg_responses.go (`controller/bg_responses.go`)

**核心变更**：Capability check 上提到 mode switch 之前（L72-77 之间），一次检查覆盖所有路径。
错误处理已由 `writeBGError`（micro-PR）统一，Phase 12 只需插入 policy check。

> **Warning**: **Policy 评估失败（err != nil）必须返回 500 internal_error**，不能映射为 403。
> 授权拒绝（!allowed）和策略引擎故障是不同的错误语义。

```diff
 // Default billing context
 canonicalReq.BillingContext = relaycommon.BillingContext{
     BillingMode: "hosted",
 }

+// Capability policy check — before mode dispatch, covers sync/async/stream
+allowed, reason, err := service.EvaluateCapabilityAccess(
+    canonicalReq.OrgID, canonicalReq.ProjectID, canonicalReq.ApiKeyID, canonicalReq.Model)
+if err != nil {
+    common.SysError("PostResponses policy evaluation failed: " + err.Error())
+    writeBGError(c, err)  // no sentinel match -> defaults to 500 internal_error
+    return
+}
+if !allowed {
+    writeBGError(c, fmt.Errorf("%w: %s", service.ErrCapabilityDenied, reason))
+    return
+}
+
 // Dispatch based on mode
 mode := "sync"
```

> **Note**: `ErrCapabilityDenied` 不会出现在 Dispatch* 的返回 error 中（已在 controller 提前拦截），
> 所以 Dispatch 的 error handler 不需要处理它。Stream 路径也无需修改。
> Service 层不做任何 defensive policy check —— capability check 仅在 controller 执行一次。

---

#### [MODIFY] bg_sessions.go (`controller/bg_sessions.go`)

**两个变更**（error 格式统一已由 micro-PR 的 `writeBGError` 完成）：

1. **给 Session 也生成 `RequestID`**（审计完整性）：

```diff
 canonicalReq := &relaycommon.CanonicalRequest{
+    RequestID:  relaycommon.GenerateResponseID(),
     ResponseID: relaycommon.GenerateResponseID(),
```

2. **Capability check 插入 `CreateSession` 前**（error 不能忽略）：

> **Warning**: `EvaluateCapabilityAccess` 的 error 代表策略引擎故障（如 DB 查询失败），
> **必须返回 500**，不能用 `_` 忽略。授权边界的评估失败不能静默通过。

```diff
 canonicalReq := &relaycommon.CanonicalRequest{...}

+// Capability policy check — error = engine failure (500), !allowed = policy deny (403)
+allowed, reason, policyErr := service.EvaluateCapabilityAccess(orgID, projectID, apiKeyID, basegateReq.Model)
+if policyErr != nil {
+    common.SysError("PostSessions policy evaluation failed: " + policyErr.Error())
+    writeBGError(c, policyErr)  // no sentinel match -> defaults to 500 internal_error
+    return
+}
+if !allowed {
+    writeBGError(c, fmt.Errorf("%w: %s", service.ErrCapabilityDenied, reason))
+    return
+}
+
 sessionResp, err := service.CreateSession(canonicalReq)
```

**不修改的路径**：
- `PostSessionAction` — capability check 不做（adapter 已绑定）
- `CloseSessionByID` — capability check 不做（清理操作）

---

### Component 7: Service 层集成

#### [MODIFY] bg_orchestrator.go (`service/bg_orchestrator.go`)

**改动范围大幅缩小**（capability check 已上提到 controller）：只替换 adapter lookup。
**Service 层不做任何 capability policy 检查**——策略评估是 controller/入口层的职责。

**DispatchSync（L62-66）**——分开处理 error 和 empty，避免吞没 ResolveRoute 的错误信息：

```diff
 // 2. Lookup adapters
-adapters := basegate.LookupAdapters(req.Model)
-if len(adapters) == 0 {
-    return nil, fmt.Errorf("no adapters found for model: %s", req.Model)
-}
+adapters, err := ResolveRoute(req.OrgID, req.ProjectID, req.ApiKeyID, req.Model)
+if err != nil {
+    return nil, fmt.Errorf("route resolution failed for %s: %w", req.Model, err)
+}
+if len(adapters) == 0 {
+    return nil, fmt.Errorf("no adapters found for model: %s", req.Model)
+}
```

**DispatchAsync（L203-207）**：同上（分开 error 和 empty 两个分支）。

---

#### [MODIFY] bg_streaming.go (`service/bg_streaming.go`)

同上，替换 `LookupAdapters` → `ResolveRoute`（分开 error 和 empty 两个分支）。

---

#### [MODIFY] bg_session_manager.go (`service/bg_session_manager.go`)

`CreateSession` 中替换 adapter lookup → `ResolveRoute`（分开 error 和 empty 两个分支）。
`ExecuteSessionAction` / `CloseSession` 不改动。

---

### Component 8: Admin API

#### [NEW] bg_policy.go (`controller/bg_policy.go`)

Admin CRUD 端点：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/bg/policies/capabilities` | 列表（支持 scope/scopeID 筛选 + 分页） |
| POST | `/api/bg/policies/capabilities` | 创建 |
| PUT | `/api/bg/policies/capabilities/:id` | 更新 |
| DELETE | `/api/bg/policies/capabilities/:id` | 删除 |
| GET | `/api/bg/policies/routing` | 列表 |
| POST | `/api/bg/policies/routing` | 创建 |
| PUT | `/api/bg/policies/routing/:id` | 更新 |
| DELETE | `/api/bg/policies/routing/:id` | 删除 |

**关键流程**：
1. 请求解析 → `model.Validate()` 校验 → CRUD 成功 → **立刻写入审计日志** → `service.InvalidatePolicyCache()`
2. **`InvalidatePolicyCache()` 返回 error 时，Admin API 返回 500**（变更已落库且已审计，但 cache 未更新，明确告知客户端）
3. Adapter 列表参考：Admin 配置 routing policy 时使用已有的 `GET /api/bg/adapters`（api-router.go:265）

```go
// Admin CRUD handler pattern:
if err := model.CreateBgCapabilityPolicy(&policy); err != nil {
    c.JSON(http.StatusInternalServerError, ...)
    return
}

// Write audit log immediately after successful mutation to prevent log drop if cache sync fails
_ = model.RecordBgAuditLog(orgID, "", "", "policy_created", map[string]interface{}{
    // policy info
})

if err := service.InvalidatePolicyCache(); err != nil {
    common.SysError("policy cache reload failed after CRUD: " + err.Error())
    c.JSON(http.StatusInternalServerError, gin.H{
        "error": gin.H{
            "code":    "cache_sync_failed",
            "type":    "api_error",
            "message": "policy saved but cache reload failed; retry or wait 60s for auto-refresh",
        },
    })
    return
}
// return success response
```

---

#### [MODIFY] api-router.go (`router/api-router.go`)

在 `bgAdminRoute` 组下注册 policy 路由（L274 之后）：

```diff
 bgAdminRoute.POST("/webhooks/:id/retry", controller.AdminRetryBgWebhookEvent)
+
+// Policy management
+bgAdminRoute.GET("/policies/capabilities", controller.AdminListBgCapabilityPolicies)
+bgAdminRoute.POST("/policies/capabilities", controller.AdminCreateBgCapabilityPolicy)
+bgAdminRoute.PUT("/policies/capabilities/:id", controller.AdminUpdateBgCapabilityPolicy)
+bgAdminRoute.DELETE("/policies/capabilities/:id", controller.AdminDeleteBgCapabilityPolicy)
+bgAdminRoute.GET("/policies/routing", controller.AdminListBgRoutingPolicies)
+bgAdminRoute.POST("/policies/routing", controller.AdminCreateBgRoutingPolicy)
+bgAdminRoute.PUT("/policies/routing/:id", controller.AdminUpdateBgRoutingPolicy)
+bgAdminRoute.DELETE("/policies/routing/:id", controller.AdminDeleteBgRoutingPolicy)
```

---

### Component 9: 审计日志集成

**Policy CRUD 审计**（requestID、responseID 传空字符串）：
```go
_ = model.RecordBgAuditLog(orgID, "", "", "policy_created", map[string]interface{}{...})
_ = model.RecordBgAuditLog(orgID, "", "", "policy_updated", map[string]interface{}{...})
_ = model.RecordBgAuditLog(orgID, "", "", "policy_deleted", map[string]interface{}{...})
```

**Dispatch deny 审计**（在 controller 的 403 分支中）：
```go
// PostResponses — canonicalReq.RequestID always populated
_ = model.RecordBgAuditLog(canonicalReq.OrgID, canonicalReq.RequestID, "", "capability_denied", map[string]interface{}{
    "model":    req.Model,
    "reason":   reason,
})

// PostSessions — canonicalReq.RequestID now also populated (new in Phase 12)
_ = model.RecordBgAuditLog(canonicalReq.OrgID, canonicalReq.RequestID, "", "capability_denied", map[string]interface{}{
    "model":    basegateReq.Model,
    "reason":   reason,
})
```

---

## 实施计划

### Day 0（前置 micro-PR, 1-2h）: 统一错误映射 + 测试基建修正

| 步骤 | 文件 | 说明 |
|------|------|------|
| 0.1 | `controller/bg_error.go` | 新建：`writeBGError(c, err)` 统一错误映射 |
| 0.2 | `controller/bg_responses.go` | 重构：error handler → `writeBGError` 调用 |
| 0.3 | `controller/bg_sessions.go` | 重构：所有 error handler → `writeBGError` 调用 |
| **0.4** | **`controller/bg_responses_test.go`** | **修正：context key `org_id`→`id`, `api_key_id`→`token_id`**（pre-existing bug） |
| 0.5 | 测试 | `go test ./controller/... -count=1` 验证无行为变化 |

> 独立 commit/PR，不混入 Phase 12 本体。
> **0.4 是关键**：不修这个 bug，Phase 12 所有 policy scope 测试都不可信。

---

### Day 1: 数据模型 + Adapter Registry + Policy 评估引擎

| 步骤 | 文件 | 说明 |
|------|------|------|
| 1.1 | `model/bg_capability_policy.go` | 新建：表定义 + CRUD + `Validate()`（enforced + MaxConcurrency=0 + wildcard 位置校验） |
| 1.2 | `model/bg_routing_policy.go` | 新建：表定义 + CRUD + `Validate()`（含 wildcard 位置校验） |
| 1.3 | `model/main.go` | 修改：AutoMigrate 注册 |
| 1.4 | `relay/basegate/adapter_registry.go` | 修改：新增 `ListAdaptersUnordered()`（锁内 copy, doc comment 标注 Phase 13 扩展点） |
| 1.5 | `service/bg_policy_engine.go` | 新建：`ErrCapabilityDenied` + `EvaluateCapabilityAccess` + `matchCapabilityPattern` + 简化缓存 + `InvalidatePolicyCache() error`（返回 error 给 Admin API） |
| 1.6 | `service/bg_policy_engine_test.go` | 新建：16 个 test case（含 MaxConcurrency 校验、wildcard 位置、delete 缓存失效、refresh 失败行为、cacheInitialized=false） |

### Day 2: Routing Policy 引擎

| 步骤 | 文件 | 说明 |
|------|------|------|
| 2.1 | `service/bg_router.go` | 新建：`ResolveRoute` + `resolveRouteWithRand` + 4 种策略实现 |
| 2.2 | `service/bg_router_test.go` | 新建：9 个 test case（固定 seed, 含 legacy wrapper 名称匹配） |
| 2.3 | `service/bg_policy_engine.go` | 补充：routing policy 缓存加载 + 匹配 |

### Day 3: Controller + Service 集成（逐路径回归 + checkpoint review）

| 步骤 | 文件 | 说明 | 验证 |
|------|------|------|------|
| 3.1 | `controller/bg_responses.go` | 修改：mode switch 前加 `EvaluateCapabilityAccess`（使用 `writeBGError`） | `go test ./controller/... -run TestBgResponses -count=1` |
| 3.2 | `controller/bg_sessions.go` | 修改：加 RequestID 生成 + capability check（使用 `writeBGError`） | `go test ./controller/... -run TestBgSession -count=1` |
| 3.3 | `service/bg_orchestrator.go` | 修改：`LookupAdapters` → `ResolveRoute`（分开 err/empty） | `go test ./service/... -run TestDispatch -count=1` |
| 3.4 | `service/bg_streaming.go` | 修改：`LookupAdapters` → `ResolveRoute`（分开 err/empty） | `go test ./service/... -run TestStream -count=1` |
| 3.5 | `service/bg_session_manager.go` | 修改：adapter lookup → `ResolveRoute`（分开 err/empty） | `go test ./controller/... -run TestBgSession -count=1` |
| 3.6 | E2E 回归 | 确保无 policy 时行为完全向后兼容 | `go test ./controller/... -run TestE2E -count=1` |
| 3.7 | `main.go` (startup) | 修改：添加 `StartPolicyCacheRefresher(60s)` | 启动日志确认 |
| **3.8** | **Checkpoint review** | **Day 3 结束时做代码 review：orchestrator 集成完成、无 policy 回归通过** | **所有 Day 1-3 测试 PASS** |

### Day 4: Admin API + 审计

| 步骤 | 文件 | 说明 |
|------|------|------|
| 4.1 | `controller/bg_policy.go` | 新建：8 个 CRUD handler（`InvalidatePolicyCache` 失败 → 500） |
| 4.2 | `router/api-router.go` | 修改：注册 policy 路由 |
| 4.3 | 审计日志集成 | CRUD + deny 审计（session deny 审计有 RequestID） |
| 4.4 | `controller/bg_policy_test.go` | Admin API 集成测试 |

### Day 5 (弹性): 全量回归 + 收尾

| 步骤 | 说明 |
|------|------|
| 5.1 | `go test ./... -race -count=1` 全量回归 |
| 5.2 | 缓存一致性验证（CRUD 同步 reload + 60s TTL 兜底） |
| 5.3 | Legacy wrapper 路由兼容性验证 |
| 5.4 | 更新 ROADMAP.md Phase 12 验收标准状态 |

---

## 验收标准

**前置 micro-PR (Day 0)**：
- [ ] **writeBGError**：所有 BaseGate controller error handler 统一使用 `writeBGError(c, err)`
- [ ] **Stream 402/409**：stream 路径下 `ErrInsufficientQuota` 返回 402（修复 pre-existing bug）
- [ ] **测试 context key 修正**：`newBgTestContext` 中 `org_id`→`id`, `api_key_id`→`token_id`
- [ ] **行为不变**：micro-PR 合入后所有现有测试 PASS

**Phase 12 本体 (Day 1-5)**：
- [ ] **无 policy 默认行为**：启动后所有请求行为不变（向后完全兼容）
- [ ] **Capability deny**：platform deny `bg.sandbox.*` → 403 `{"error":{"code":"capability_denied","type":"permission_error",...}}`
- [ ] **Policy 评估失败**：`EvaluateCapabilityAccess` 返回 error 时，controller 返回 500 internal_error（非 403）
- [ ] **Enforced deny**：platform deny (enforced=true) + org allow → 仍然 deny
- [ ] **Enforced 校验**：`Validate()` 拒绝 scope != platform && enforced=true
- [ ] **MaxConcurrency 校验**：`Validate()` 拒绝 MaxConcurrency != 0（reserved 字段）
- [ ] **Wildcard 位置校验**：`Validate()` 拒绝 `*` 不在最后一段（如 `bg.*.chat`）
- [ ] **Scope override**：platform deny (enforced=false) + org allow → allow
- [ ] **Specificity 优先**：org deny `bg.sandbox.*` + project allow `bg.sandbox.session.standard` → allow
- [ ] **Stream 路径**：stream 模式下 capability denied 返回 403（controller 提前拦截，在 SSE 写出前）
- [ ] **Stream SSE 守卫**：stream 已开始写 SSE 后发生错误时，不追加 JSON body，只记录日志（`!c.Writer.Written()` 守卫）
- [ ] **ResolveRoute 错误保留**：routing error 通过 `%w` 传播，不被 "no adapters found" 吞没
- [ ] **Routing fixed**：fixed 策略锁定到指定 adapter
- [ ] **Routing weighted**：确定性排序正确（固定 seed 测试）
- [ ] **Routing primary_backup**：返回 [primary, fallback...] 顺序
- [ ] **Legacy wrapper 匹配**：routing policy 能正确匹配 `legacy_task_suno`、`legacy_task_kling` 格式
- [ ] **Admin API**：CRUD policy 全部可用，`Validate()` 在 model 层执行
- [ ] **Admin API cache 失败**：`InvalidatePolicyCache` 失败时 Admin API 返回 500（CRUD 已提交但 cache 未同步）
- [ ] **Cache**：启动全量加载 + CRUD 同步 reload + 60s 兜底
- [ ] **Cache fallback**：无可用 cache 前，`EvaluateCapabilityAccess` 返回 internal err，不 fail-open
- [ ] **Cache 启动**：`StartPolicyCacheRefresher` 在 startup 中同步加载，成功后启动 ticker。加载失败记录日志。
- [ ] **Cache 失败**：定时 refresh 失败时保留旧缓存 + 打日志，不 crash
- [ ] **Cache 删除**：policy delete 后 `InvalidatePolicyCache` 立即生效
- [ ] **Session 路径**：CreateSession 走 policy + route；ExecuteAction/CloseSession 不走
- [ ] **Session policy error**：PostSessions 中 `EvaluateCapabilityAccess` 失败返回 500（不忽略 error）
- [ ] **Session RequestID**：PostSessions 生成 RequestID，审计记录中可查
- [ ] **审计**：policy CRUD 和 deny 事件均有审计记录（session deny 有 RequestID）
- [ ] **审计时序**：CRUD 成功后先写审计日志，再 reload cache（确保 cache 失败不丢审计）
- [ ] **Checkpoint**：Day 3 结束时通过 checkpoint review
- [ ] **全量回归**：`go test ./... -race -count=1` 全部 PASS

---

## 文件变更清单

### 前置 micro-PR (Day 0)

| 文件 | 操作 | 说明 |
|------|------|------|
| `controller/bg_error.go` | **新建** | `writeBGError(c, err)` 统一错误映射 |
| `controller/bg_responses.go` | 重构 | error handler → `writeBGError` |
| `controller/bg_sessions.go` | 重构 | error handler → `writeBGError` |
| `controller/bg_responses_test.go` | **修正** | context key `org_id`→`id`, `api_key_id`→`token_id` |

### Phase 12 本体 (Day 1-5)

| 文件 | 操作 | 说明 |
|------|------|------|
| `model/bg_capability_policy.go` | **新建** | 表定义 + CRUD + Validate()（enforced + MaxConcurrency=0 + wildcard 位置） |
| `model/bg_routing_policy.go` | **新建** | 表定义 + CRUD + Validate()（含 wildcard 位置校验） |
| `model/main.go` | 修改 | AutoMigrate 注册 |
| `relay/basegate/adapter_registry.go` | 修改 | `ListAdaptersUnordered()`（锁内 copy, Phase 13 doc comment） |
| `service/bg_policy_engine.go` | **新建** | EvaluateCapabilityAccess + 简化缓存 + `InvalidatePolicyCache() error` |
| `service/bg_policy_engine_test.go` | **新建** | 16 个 test case（含缓存行为 + wildcard 位置 + cacheInitialized） |
| `service/bg_router.go` | **新建** | ResolveRoute + resolveRouteWithRand + 4 策略 |
| `service/bg_router_test.go` | **新建** | 9 个 test case（含 `legacy_task_suno` 匹配） |
| `controller/bg_responses.go` | 修改 | 加 capability check（err→500, !allowed→403） |
| `controller/bg_sessions.go` | 修改 | 加 RequestID + capability check（err→500, !allowed→403） |
| `service/bg_orchestrator.go` | 修改 | LookupAdapters → ResolveRoute（分开 err/empty） |
| `service/bg_streaming.go` | 修改 | LookupAdapters → ResolveRoute（分开 err/empty） |
| `service/bg_session_manager.go` | 修改 | adapter lookup → ResolveRoute（分开 err/empty） |
| `controller/bg_policy.go` | **新建** | Admin CRUD API（InvalidatePolicyCache 失败 → 500） |
| `controller/bg_policy_test.go` | **新建** | Admin API 集成测试 |
| `router/api-router.go` | 修改 | 注册 policy 路由 |
| `main.go` (startup) | 修改 | `StartPolicyCacheRefresher(60s)` |

**总计**：micro-PR 1 新 + 3 改 | Phase 12 本体 8 新 + 8 改

---

## 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| Policy 评估性能 | 延迟增加 | 内存缓存，O(N) where N < 100 |
| 自定义 glob `*` 跨段语义 | 错误 allow/deny | 16 个测试 + Validate() 拒绝 `*` 非末尾 + 文档标注"非标准 glob" |
| Legacy wrapper 名称格式 | Fixed 策略找不到 adapter | 命名格式 `legacy_task_<platform>`（如 `legacy_task_suno`）已验证 + 专门测试 |
| 缓存 refresh 失败 | 使用过期 policy | 定时 refresh：保留旧缓存 + 日志；CRUD reload：返回 500 给 Admin API |
| 启动时 DB 不可用 | fail-open 越权 | `cacheInitialized` atomic.Bool 守卫，首刷成功前所有请求返回 500 |
| EvaluateCapabilityAccess 引擎故障 | 授权判断不可用 | controller 返回 500 internal_error（非 403），error 不忽略 |
| 测试 context key 不一致 | policy scope 测试全部无效 | micro-PR 中修正 `id`/`token_id`（pre-existing bug） |
| ResolveRoute 错误被吞没 | 用户看不到真实原因 | 分开 err/empty，`%w` 保留原始错误 |
| Reconciliation 不感知 policy | 无冲突 | Policy 在 response 创建前拦截 |
| Enforced deny 误配置 | 全平台能力不可用 | Validate() fail early + 审计日志 |
| 未来新入口绕过 policy check | 未授权访问 | 风险已知，当前只有 2 个 controller 入口；入口增多时考虑 flag 断言 |
| CRUD 审计丢失 | cache reload 失败时无审计记录 | 审计在 CRUD 成功后立即写入，先于 cache reload |
