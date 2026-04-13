# Phase 14: 开发者体验与前端改造 (v7 — 最终版)

v6 → v7：3 项安全/一致性修正 + 2 项实现精度补齐。核心架构不变。

---

## 0. 版本差异

### v6 → v7

| # | v6 缺陷 | v7 修正 |
|---|---------|---------|
| 1 | Relay 侧 5 个资源端点（GET/Cancel Response, GET/Action/Close Session）无 OrgID 归属校验 → 跨租户 IDOR | Service 层函数统一增加 `orgID int` 参数，查出资源后校验 `resource.OrgID == orgID` |
| 2 | `resolveProjectID` 绑定分支 audit 比对只走 `GetBgProjectByProjectID`，数字兼容 header 会误报 mismatch | Audit 比对逻辑增加 Atoi fallback，同一 project 的数字形式不触发误报 |
| 3 | `DevListBgApiKeys` 过滤参数 `?bg_project_id=<内部int64>` 暴露内部 PK | 改为 `?project_id=proj_xxx`（公开标识符），controller 层解析 + Org 归属校验 |
| 4 | `DevCreateBgProject` 无数量限制 | 新增 `model.CountBgProjectsByOrgID`，controller 层检查上限（默认 20） |
| 5 | Pricing enrichment 在 admin 和 dev controller 中重复 | 提取共享 `enrichCapabilitiesWithPricing` helper |

### v5 → v6（保留）

| # | 修正 |
|---|------|
| 1 | `resolveProjectID` 所有路径强制 `project.OrgID == c.GetInt("id")` |
| 2 | Header 存在但无法解析 → 返回 error，controller 返回 400 |
| 3 | `DevListBgApiKeys` DB 级 `bg_project_id` 过滤 |
| 4 | `DevListBgCapabilities` pricing enrichment |

### v3 → v5（保留）

| # | 修正 |
|---|------|
| 1 | TokenAuth 中间件新增 Token→Project 绑定校验逻辑 |
| 2 | 外部 API 统一传公开 `project_id string`，controller 层解析为内部 ID |
| 3 | `DevListBgCapabilities` 使用 `GetActiveBgCapabilities()` |
| 4 | Schema 预览降级：只展示 `description + supported_modes + billable_unit` |
| 5 | `DevRevealBgApiKey` 路由带 `CriticalRateLimit + DisableCache` |

---

## 1. Relay 侧资源归属校验 (v7 新增)

### 现状问题

以下 5 个 relay 端点（`relay-router.go:120-128`）通过 TokenAuth 鉴权后，service 层直接按 resource ID 查库，**不校验资源是否属于当前 token 的 OrgID**：

| 端点 | Controller | Service | 问题 |
|------|-----------|---------|------|
| `GET /v1/bg/responses/:id` | `bg_responses.go:132` | `bg_orchestrator.go:391` | `GetResponse(responseID)` 无 org 校验 |
| `POST /v1/bg/responses/:id/cancel` | `bg_responses.go:159` | `bg_orchestrator.go:400` | `CancelResponse(responseID)` 无 org 校验 |
| `GET /v1/bg/sessions/:id` | `bg_sessions.go:74` | `bg_session_manager.go:154` | `GetSession(sessionID)` 无 org 校验 |
| `POST /v1/bg/sessions/:id/action` | `bg_sessions.go:93` | `bg_session_manager.go:163` | `ExecuteSessionAction(sessionID, req)` 无 org 校验 |
| `POST /v1/bg/sessions/:id/close` | `bg_sessions.go:120` | `bg_session_manager.go:262` | `CloseSession(sessionID)` 无 org 校验 |

`BgResponse.OrgID`（`bg_response.go:67`）和 `BgSession.OrgID`（`bg_session.go:62`）字段已存在，只是查询后没有比对。

### 修复方案

#### [MODIFY] service/bg_orchestrator.go — `GetResponse()`, `CancelResponse()`

```go
// 改前：func GetResponse(responseID string) (*dto.BaseGateResponse, error)
// 改后：
func GetResponse(responseID string, orgID int) (*dto.BaseGateResponse, error) {
    bgResp, err := model.GetBgResponseByResponseID(responseID)
    if err != nil {
        return nil, fmt.Errorf("response not found: %w", err)
    }
    if bgResp.OrgID != orgID {
        return nil, fmt.Errorf("response not found: %s", responseID)
    }
    return buildResponseFromDB(bgResp)
}

// 改前：func CancelResponse(responseID string) (*dto.BaseGateResponse, error)
// 改后：
func CancelResponse(responseID string, orgID int) (*dto.BaseGateResponse, error) {
    bgResp, err := model.GetBgResponseByResponseID(responseID)
    if err != nil {
        return nil, fmt.Errorf("response not found: %w", err)
    }
    if bgResp.OrgID != orgID {
        return nil, fmt.Errorf("response not found: %s", responseID)
    }
    // ... rest unchanged
}
```

不匹配时返回 "not found" 而非 "forbidden"，避免泄露资源存在性。

#### [MODIFY] service/bg_session_manager.go — `GetSession()`, `ExecuteSessionAction()`, `CloseSession()`

```go
// 改前：func GetSession(sessionID string) (*dto.BGSessionResponse, error)
// 改后：
func GetSession(sessionID string, orgID int) (*dto.BGSessionResponse, error) {
    bgSess, err := model.GetBgSessionBySessionID(sessionID)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrSessionNotFound, err)
    }
    if bgSess.OrgID != orgID {
        return nil, ErrSessionNotFound
    }
    return buildSessionResponseFromDB(bgSess)
}

// 改前：func ExecuteSessionAction(sessionID string, req *dto.BGSessionActionRequest) (...)
// 改后：
func ExecuteSessionAction(sessionID string, orgID int, req *dto.BGSessionActionRequest) (*dto.BGSessionActionResponse, error) {
    // ... idempotency check unchanged ...
    bgSess, err := model.GetBgSessionBySessionID(sessionID)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrSessionNotFound, err)
    }
    if bgSess.OrgID != orgID {
        return nil, ErrSessionNotFound
    }
    // ... rest unchanged
}

// 改前：func CloseSession(sessionID string) (*dto.BGSessionResponse, error)
// 改后：
func CloseSession(sessionID string, orgID int) (*dto.BGSessionResponse, error) {
    bgSess, err := model.GetBgSessionBySessionID(sessionID)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrSessionNotFound, err)
    }
    if bgSess.OrgID != orgID {
        return nil, ErrSessionNotFound
    }
    // ... rest unchanged
}
```

注意：`CloseSession` 内部递归调用 `GetSession(sessionID)` 的位置（`bg_session_manager.go:283`）也需要传入 orgID。

#### [MODIFY] controller/bg_responses.go — `GetResponseByID()`, `CancelResponseByID()`

```go
func GetResponseByID(c *gin.Context) {
    responseID := c.Param("id")
    // ... validation unchanged ...
    orgID := c.GetInt("id")
    resp, err := service.GetResponse(responseID, orgID)
    // ... error handling unchanged ...
}

func CancelResponseByID(c *gin.Context) {
    responseID := c.Param("id")
    // ... validation unchanged ...
    orgID := c.GetInt("id")
    resp, err := service.CancelResponse(responseID, orgID)
    // ... error handling unchanged ...
}
```

#### [MODIFY] controller/bg_sessions.go — `GetSessionByID()`, `PostSessionAction()`, `CloseSessionByID()`

同理，每个 handler 增加 `orgID := c.GetInt("id")` 并传入 service 函数。

---

## 2. Token → Project 运行时绑定

### 设计规则（不变）

```
token.BgProjectID > 0: 忽略 header，强制使用绑定值，header 不匹配时写 audit
token.BgProjectID == 0: 从 header 解析（见 §3）
```

### 实现

#### [MODIFY] middleware/auth.go — `SetupContextForToken()`

在 `SetupContextForToken()` 末尾（`auth.go:391` 之后）新增：

```go
// Enforce Token→Project binding for BaseGate requests.
if token.BgProjectID > 0 {
    c.Set("bg_bound_project_id", token.BgProjectID)
}
```

---

## 3. resolveProjectID（含 v7 audit 修正）

#### [NEW] controller/bg_project_resolve.go

```go
package controller

import (
    "fmt"
    "strconv"

    "github.com/QuantumNous/new-api/model"
    "github.com/gin-gonic/gin"
)

// resolveProjectID resolves the effective project ID for a BaseGate request.
//
// Returns (projectID int, err error):
//   - header absent       → (0, nil)      — no project scope
//   - header present+valid → (resolved, nil)
//   - header present+invalid → (0, error)  — caller should 400
//
// All paths enforce org ownership (project.OrgID == current user).
// Returns int (not int64) to match CanonicalRequest.ProjectID.
// Truncation from int64 is intentional: project counts will not overflow int.
func resolveProjectID(c *gin.Context) (int, error) {
    orgID := c.GetInt("id")

    // 1. Token binding takes absolute precedence
    if bound, exists := c.Get("bg_bound_project_id"); exists {
        boundID := bound.(int64)

        // v7: Audit mismatch — compare using both public string and numeric fallback
        //     to avoid false positives during backward-compat period.
        if headerVal := c.GetHeader("X-Project-Id"); headerVal != "" {
            headerInternalID := resolveHeaderToInternalID(headerVal)
            if headerInternalID != boundID {
                _ = model.RecordBgAuditLog(orgID, "", "", "project_binding_mismatch", map[string]interface{}{
                    "bound_project_id": boundID,
                    "header_value":     headerVal,
                })
            }
        }
        return int(boundID), nil
    }

    // 2. No header → no project scope (valid)
    headerVal := c.GetHeader("X-Project-Id")
    if headerVal == "" {
        return 0, nil
    }

    // 3. Try public identifier (e.g. "proj_xxxx")
    project, err := model.GetBgProjectByProjectID(headerVal)
    if err == nil {
        if project.OrgID != orgID {
            return 0, fmt.Errorf("project %s does not belong to current org", headerVal)
        }
        return int(project.ID), nil
    }

    // 4. Backward-compat: numeric string, with org ownership check
    if numID, parseErr := strconv.Atoi(headerVal); parseErr == nil {
        proj, dbErr := model.GetBgProjectByID(int64(numID))
        if dbErr == nil && proj.OrgID == orgID {
            return numID, nil
        }
        return 0, fmt.Errorf("project %s not found or does not belong to current org", headerVal)
    }

    // 5. Header present but unparseable → error (not silent 0)
    return 0, fmt.Errorf("invalid X-Project-Id: %s", headerVal)
}

// resolveHeaderToInternalID is a best-effort helper for audit comparison only.
// Returns the internal project ID if resolvable, otherwise -1.
func resolveHeaderToInternalID(headerVal string) int64 {
    // Try public identifier first
    project, err := model.GetBgProjectByProjectID(headerVal)
    if err == nil {
        return project.ID
    }
    // Try numeric
    if numID, err := strconv.Atoi(headerVal); err == nil {
        return int64(numID)
    }
    return -1
}
```

#### [MODIFY] controller/bg_responses.go:39

```diff
-projectID, _ := strconv.Atoi(c.GetHeader("X-Project-Id"))
+projectID, projErr := resolveProjectID(c)
+if projErr != nil {
+    c.JSON(http.StatusBadRequest, gin.H{
+        "error": gin.H{
+            "code":    "invalid_project",
+            "message": projErr.Error(),
+        },
+    })
+    return
+}
```

#### [MODIFY] controller/bg_sessions.go:32

同上改法。

---

## 4. Model 变更

#### [MODIFY] model/token.go

Token struct 新增字段：

```go
BgProjectID int64 `json:"bg_project_id" gorm:"index;default:0"` // immutable after creation
```

**不加入** `Update()` 的 Select 列表（`token.go:306`）。绑定在创建时设定，不可更改。

新增查询方法：

```go
// GetUserTokensByBgProjectID returns paginated tokens for a user filtered by bg_project_id.
func GetUserTokensByBgProjectID(userId int, bgProjectID int64, startIdx, num int) ([]*Token, error) {
    var tokens []*Token
    err := DB.Where("user_id = ? AND bg_project_id = ?", userId, bgProjectID).
        Order("id desc").Limit(num).Offset(startIdx).Find(&tokens).Error
    return tokens, err
}

// CountUserTokensByBgProjectID counts tokens for a user filtered by bg_project_id.
func CountUserTokensByBgProjectID(userId int, bgProjectID int64) (int64, error) {
    var count int64
    err := DB.Model(&Token{}).Where("user_id = ? AND bg_project_id = ?", userId, bgProjectID).Count(&count).Error
    return count, err
}
```

GORM AutoMigrate 自动加列，默认 0，向后兼容。

#### [MODIFY] model/bg_capability.go

预留 schema 字段（本 Phase 不填充）：

```go
InputSchemaJSON  string `json:"input_schema_json,omitempty" gorm:"type:text"`  // Phase 15
OutputSchemaJSON string `json:"output_schema_json,omitempty" gorm:"type:text"` // Phase 15
```

#### [MODIFY] model/bg_project.go

新增：

```go
// GetBgProjectByID looks up a project by its internal auto-increment PK.
func GetBgProjectByID(id int64) (*BgProject, error) {
    var project BgProject
    err := DB.Where("id = ?", id).First(&project).Error
    return &project, err
}

// CountBgProjectsByOrgID counts projects for a given org.
func CountBgProjectsByOrgID(orgID int) (int64, error) {
    var count int64
    err := DB.Model(&BgProject{}).Where("org_id = ?", orgID).Count(&count).Error
    return count, err
}
```

---

## 5. Developer-Scoped API 层

### 路由注册

#### [MODIFY] router/api-router.go

在 `bgAdminRoute` 块（`api-router.go:284`）之后新增：

```go
bgDevRoute := apiRouter.Group("/bg/dev")
bgDevRoute.Use(middleware.UserAuth())
{
    // API Keys
    bgDevRoute.GET("/apikeys", controller.DevListBgApiKeys)
    bgDevRoute.POST("/apikeys", controller.DevCreateBgApiKey)
    bgDevRoute.DELETE("/apikeys/:id", controller.DevDeleteBgApiKey)
    bgDevRoute.POST("/apikeys/:id/reveal",
        middleware.CriticalRateLimit(),
        middleware.DisableCache(),
        controller.DevRevealBgApiKey)

    // Projects (user-scoped)
    bgDevRoute.GET("/projects", controller.DevListBgProjects)
    bgDevRoute.POST("/projects", controller.DevCreateBgProject)

    // Usage & Responses (user-scoped, read-only)
    bgDevRoute.GET("/usage", controller.DevGetBgUsage)
    bgDevRoute.GET("/responses", controller.DevListBgResponses)

    // Capabilities (read-only, active-only)
    bgDevRoute.GET("/capabilities", controller.DevListBgCapabilities)
}
```

### Controller 实现

#### [NEW] controller/bg_apikey.go

| 方法 | 说明 |
|------|------|
| `DevListBgApiKeys` | `userId := c.GetInt("id")`。**v7**: 过滤参数为 `?project_id=proj_xxx`（公开标识符）。无参数时用 `GetAllUserTokens`；有参数时 controller 先 `GetBgProjectByProjectID(projectIDStr)` 解析 + 校验 `project.OrgID == userId`，再用 `GetUserTokensByBgProjectID(userId, project.ID, ...)` DB 级过滤。返回掩码 key |
| `DevCreateBgApiKey` | 接受 `project_id string`（公开标识符），controller 调 `GetBgProjectByProjectID` 解析，校验 `project.OrgID == c.GetInt("id")` 防跨租户。创建后一次性返回明文 key |
| `DevDeleteBgApiKey` | 校验 `token.UserId == c.GetInt("id")` |
| `DevRevealBgApiKey` | 路由带 `CriticalRateLimit + DisableCache`，校验归属后返回明文 |

`DevListBgApiKeys` 核心逻辑：

```go
func DevListBgApiKeys(c *gin.Context) {
    userId := c.GetInt("id")
    pageInfo := common.GetPageQuery(c)
    startIdx := pageInfo.GetStartIdx()
    num := pageInfo.GetPageSize()

    // v7: 过滤参数使用公开 project_id，controller 层解析为内部 ID
    projectIDStr := c.Query("project_id")
    if projectIDStr != "" {
        project, err := model.GetBgProjectByProjectID(projectIDStr)
        if err != nil || project.OrgID != userId {
            common.ApiErrorMsg(c, "project not found or not owned")
            return
        }
        tokens, err := model.GetUserTokensByBgProjectID(userId, project.ID, startIdx, num)
        if err != nil {
            common.ApiErrorMsg(c, "failed to list keys: "+err.Error())
            return
        }
        total, _ := model.CountUserTokensByBgProjectID(userId, project.ID)
        for _, t := range tokens { t.Key = t.GetMaskedKey() }
        pageInfo.SetTotal(int(total))
        pageInfo.SetItems(tokens)
        common.ApiSuccess(c, pageInfo)
        return
    }

    // No project filter — return all tokens for user
    tokens, err := model.GetAllUserTokens(userId, startIdx, num)
    // ... mask keys, set total, return ...
}
```

#### [NEW] controller/bg_dev_project.go

| 方法 | 说明 |
|------|------|
| `DevListBgProjects` | 强制 `orgID = c.GetInt("id")`，复用 `model.ListBgProjectsByOrgID` |
| `DevCreateBgProject` | OrgID 强制 = `c.GetInt("id")`。**v7**: 创建前检查数量上限 |

`DevCreateBgProject` 上限检查：

```go
const maxProjectsPerOrg = 20

func DevCreateBgProject(c *gin.Context) {
    orgID := c.GetInt("id")

    count, err := model.CountBgProjectsByOrgID(orgID)
    if err != nil {
        common.ApiErrorMsg(c, "failed to check project count")
        return
    }
    if count >= maxProjectsPerOrg {
        common.ApiErrorMsg(c, fmt.Sprintf("project limit reached (max %d)", maxProjectsPerOrg))
        return
    }

    // ... bind request, set OrgID = orgID, create ...
}
```

#### [NEW] controller/bg_dev_usage.go

| 方法 | 说明 |
|------|------|
| `DevGetBgUsage` | 复用 `GetBgUsage` 查询逻辑，从 UserAuth 的 `c.GetInt("id")` 获取 orgID（UserAuth 和 TokenAuth 都设置相同 context key `"id"`，见 `auth.go:132` 和 `auth.go:376`） |

#### [NEW] controller/bg_dev_responses.go

| 方法 | 说明 |
|------|------|
| `DevListBgResponses` | 强制 `orgID = c.GetInt("id")`，复用现有查询逻辑 |

#### [NEW] controller/bg_capability_helper.go — Pricing enrichment 共享 helper (v7 新增)

从 `bg_admin.go:186-221` 提取，admin 和 dev 共用：

```go
package controller

import (
    "github.com/QuantumNous/new-api/model"
    "github.com/QuantumNous/new-api/service"
)

// CapabilityWithPricing extends BgCapability with live pricing info from ratio_setting.
type CapabilityWithPricing struct {
    model.BgCapability
    PricingMode string  `json:"pricing_mode"` // "ratio" | "price" | "none"
    UnitPrice   float64 `json:"unit_price"`
}

// enrichCapabilitiesWithPricing applies live pricing lookup to a list of capabilities.
func enrichCapabilitiesWithPricing(caps []*model.BgCapability) []CapabilityWithPricing {
    enriched := make([]CapabilityWithPricing, len(caps))
    for i, cap := range caps {
        enriched[i].BgCapability = *cap
        pricing := service.LookupPricing(cap.CapabilityName, "hosted")
        if pricing != nil && pricing.UnitPrice > 0 {
            if pricing.PricingMode == "per_call" {
                enriched[i].PricingMode = "price"
            } else {
                enriched[i].PricingMode = "ratio"
            }
            enriched[i].UnitPrice = pricing.UnitPrice
        } else {
            enriched[i].PricingMode = "none"
        }
    }
    return enriched
}
```

#### [MODIFY] controller/bg_admin.go — `AdminListBgCapabilities()`

将 `bg_admin.go:186-221` 中内联的 struct 和 enrichment 逻辑替换为调用共享 helper：

```go
func AdminListBgCapabilities(c *gin.Context) {
    pageInfo := common.GetPageQuery(c)
    capabilities, total, err := model.GetBgCapabilitiesAdmin(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
    if err != nil {
        common.ApiErrorMsg(c, "Failed to list capabilities: "+err.Error())
        return
    }
    enriched := enrichCapabilitiesWithPricing(capabilities)
    pageInfo.SetTotal(int(total))
    pageInfo.SetItems(enriched)
    common.ApiSuccess(c, pageInfo)
}
```

#### [NEW] controller/bg_dev_capabilities.go

```go
// DevListBgCapabilities — GET /api/bg/dev/capabilities
// Returns ACTIVE capabilities enriched with live pricing.
// Difference from admin: uses GetActiveBgCapabilities (status='active' only), no pagination.
func DevListBgCapabilities(c *gin.Context) {
    caps, err := model.GetActiveBgCapabilities()
    if err != nil {
        common.ApiErrorMsg(c, "Failed to list capabilities: "+err.Error())
        return
    }
    enriched := enrichCapabilitiesWithPricing(caps)
    common.ApiSuccess(c, enriched)
}
```

---

## 6. X-Project-Id 公开标识符方案

**外部 API 契约**：`X-Project-Id` header 接受 `BgProject.ProjectID string`（如 `proj_abcdef123`）。向后兼容接受纯数字（内部 PK），但所有路径强制 Org 归属校验。

**Developer API 全面统一**：
- 创建 Key：`project_id string`（公开）
- 列表过滤：`?project_id=proj_xxx`（公开，v7 修正）
- Playground 代码生成：`X-Project-Id: proj_xxx`（公开）

**运行时管线不变**：`CanonicalRequest.ProjectID int` 及下游所有 model 仍使用 int。

---

## 7. Frontend 变更

### 7.1 路由与菜单拆分

#### [MODIFY] App.jsx

**PrivateRoute（普通用户可访问）**：
- `/console/bg-dev-dashboard` → `BgDevDashboard`（**新页面**，admin BgDashboard 保持不变）
- `/console/bg-apikeys` → `BgApiKeys`（**新页面**）
- `/console/bg-capabilities` → `BgCapabilities`（增强版，**数据源按角色判断**）
- `/console/bg-playground` → `BgPlayground`

**AdminRoute（管理员）**：保留所有现有 BG admin 页面 + 新增 `/console/bg-policies`。

#### [MODIFY] SiderBar.jsx

- 新增 `bgDevItems` 数组（无 `isAdmin()` 限制）：`bgDevDashboard`、`bgApiKeys`、`bgCapabilities`、`bgPlayground`。
- 新增侧边栏分组 "BaseGate 开发者"。
- Admin 侧边栏分组保留不变（已有的 `bgDashboard` 等不移除，管理员同时看到两个分组）。

### 7.2 页面

#### [NEW] web/src/pages/BgDevDashboard/index.jsx
- 数据源：`/api/bg/dev/usage` + `/api/bg/dev/responses`。
- 展示：今日概览、最近 50 条请求、趋势图。
- Admin `BgDashboard` 保持不变，走 admin API。

#### [NEW] web/src/pages/BgApiKeys/index.jsx
- 数据源：`/api/bg/dev/apikeys`、`/api/bg/dev/projects`。
- 创建弹窗：Project 下拉使用公开 `project_id` 显示。
- **v7**: 列表过滤也使用公开 `project_id`（`?project_id=proj_xxx`）。
- 创建成功弹窗一次性展示明文 key + 复制按钮。

#### [MODIFY] web/src/pages/BgCapabilities/index.jsx
- **数据源按角色判断**：admin 走 `/api/bg/capabilities`（含停用能力），非 admin 走 `/api/bg/dev/capabilities`（active-only）。
- "Schema 预览"降级：展示 `description` + `supported_modes` + `billable_unit` + `pricing_mode` + `unit_price` 的结构化卡片。Phase 15 补齐 `input_schema_json`/`output_schema_json` 后升级。

#### [MODIFY] web/src/pages/BgPlayground/index.jsx
- 代码生成面板：生成的 `X-Project-Id` header 使用公开 `project_id`（`proj_xxx`），不暴露内部 PK。

#### [NEW] web/src/pages/BgPolicies/CapabilityPolicies.jsx
#### [NEW] web/src/pages/BgPolicies/RoutingPolicies.jsx
- 调用现有 admin API `/api/bg/policies/capabilities` 和 `/api/bg/policies/routing`。
- AdminRoute 包裹。

---

## 8. 执行顺序

| Step | 内容 | 估时 |
|------|------|------|
| **S1** | `model/token.go` — `BgProjectID int64` + 查询方法 | 1h |
| **S2** | `model/bg_capability.go` — 预留 schema 字段 | 0.5h |
| **S3** | `model/bg_project.go` — `GetBgProjectByID` + `CountBgProjectsByOrgID` | 0.5h |
| **S4** | `controller/bg_project_resolve.go` — `resolveProjectID`（含 org 校验 + v7 audit 修正 + 400 error） | 1.5h |
| **S5** | `middleware/auth.go` — `SetupContextForToken` 绑定注入 | 0.5h |
| **S6** | `controller/bg_responses.go` + `bg_sessions.go` — 替换 ProjectID 解析（处理 error→400） | 0.5h |
| **S7** | **v7**: `service/bg_orchestrator.go` + `service/bg_session_manager.go` — 5 个函数增加 `orgID` 参数 + 归属校验 | 1.5h |
| **S8** | **v7**: `controller/bg_responses.go` + `controller/bg_sessions.go` — 对应 5 个 handler 传入 `orgID` | 0.5h |
| **S9** | `controller/bg_capability_helper.go` — 提取共享 enrichment helper + 修改 `bg_admin.go` 调用 | 0.5h |
| **S10** | `controller/bg_apikey.go` — DevList（公开 project_id 过滤）/Create（org 校验）/Delete/Reveal | 3h |
| **S11** | `controller/bg_dev_project.go`（含 limit）+ `bg_dev_usage.go` + `bg_dev_responses.go` | 2h |
| **S12** | `controller/bg_dev_capabilities.go` — active-only + enrichment helper | 0.5h |
| **S13** | `router/api-router.go` — 注册 `/api/bg/dev/` 路由组 | 0.5h |
| **S14** | `go build` + `go test` — 后端全绿 | 0.5h |
| **S15** | 前端：`App.jsx` + `SiderBar.jsx` 路由拆分 | 1h |
| **S16** | 前端：`BgApiKeys/index.jsx`（公开 project_id 过滤） | 3h |
| **S17** | 前端：`BgDevDashboard/index.jsx` | 2h |
| **S18** | 前端：`BgCapabilities` 增强（角色判断数据源 + pricing + 降级 schema） | 1.5h |
| **S19** | 前端：`BgPlayground` 代码生成 | 2h |
| **S20** | 前端：`BgPolicies` 页面 | 2h |

**总计约 ~25h**（较 v6 增加 ~2h，主要来自 S7-S9）。

---

## Verification Plan

### Automated Tests

#### [NEW] controller/bg_project_resolve_test.go
- 传公开 `proj_xxx` 属于自己 → 返回 ID。
- 传公开 `proj_xxx` 属于别人 → error（跨租户拒绝）。
- 传无效字符串 `abc123` → error（而非静默 0）。
- `X-Project-Id` 缺失 → `(0, nil)`。
- Token 有绑定 + header 不匹配 → 返回绑定值 + audit log 写入。
- **v7**: Token 有绑定 + header 传同一 project 的数字 ID → 返回绑定值 + **无** audit log（无误报）。
- Atoi fallback `42` 属于自己 → OK。
- Atoi fallback `42` 属于别人 → error。

#### [NEW] service/bg_orchestrator_ownership_test.go (v7 新增)
- `GetResponse(responseID, orgID=owner)` → 成功。
- `GetResponse(responseID, orgID=other)` → "not found" error（不泄露存在性）。
- `CancelResponse(responseID, orgID=other)` → "not found" error。

#### [NEW] service/bg_session_ownership_test.go (v7 新增)
- `GetSession(sessionID, orgID=owner)` → 成功。
- `GetSession(sessionID, orgID=other)` → ErrSessionNotFound。
- `ExecuteSessionAction(sessionID, orgID=other, req)` → ErrSessionNotFound。
- `CloseSession(sessionID, orgID=other)` → ErrSessionNotFound。

#### [NEW] controller/bg_apikey_test.go
- 创建 Key 关联别人的 `proj_xxx` → 400。
- 创建 Key 关联自己的 `proj_xxx` → 成功，返回明文 key。
- List 不带 `project_id` → 返回所有 token。
- **v7**: List 带 `?project_id=proj_xxx` → DB 级过滤，pagination 正确。
- **v7**: List 带 `?project_id=<别人的proj>` → error。

#### [NEW] controller/bg_dev_project_test.go (v7 新增)
- 创建 project 超过上限（20）→ 400。

#### [NEW] controller/bg_dev_capabilities_test.go
- 返回的每个 capability 包含 `pricing_mode` 和 `unit_price` 字段。
- 只返回 `status='active'` 的 capabilities。

### Manual Verification

1. **Relay IDOR 验证**（v7 核心）：用 Token A（org=1）创建 response → 用 Token B（org=2）调 `GET /v1/bg/responses/:id` → 404（不是 200）。
2. 用绑定了 Project A 的 Token 调 `/v1/bg/responses`，传 `X-Project-Id: proj_B` → 仍走 A + audit log。
3. **v7**: 用绑定了 Project A 的 Token，传 `X-Project-Id: 42`（42 = Project A 的内部 ID）→ 仍走 A + **无** audit log。
4. 用未绑定 Token，传 `X-Project-Id: proj_xxx`（属于别人）→ 400。
5. 用未绑定 Token，传 `X-Project-Id: garbage` → 400（不是静默 0）。
6. 在 BgApiKeys 页面按 project 过滤 → URL query 为 `?project_id=proj_xxx`（不暴露内部 ID）。
7. 在 BgApiKeys 创建 Key 时选择别人的 Project → 后端拒绝。
8. 非 admin 用户看到侧边栏"BaseGate 开发者"分组，看不到 admin 页面。
9. Admin 用户同时看到两个分组。
10. 在 BgCapabilities 页面看到定价信息（pricing_mode + unit_price）。
11. 尝试创建第 21 个 Project → 400。
12. 在 BgPlayground 代码生成中 `X-Project-Id` 使用公开 `proj_xxx` 标识符。
