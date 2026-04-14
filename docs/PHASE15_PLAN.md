# Phase 15: Capability → Tool 投影与 Python SDK (v1)

Phase 14 交付了完整的开发者前端。Phase 15 让 BaseGate 能力被 Agent 框架直接发现和调用，
并提供 Python SDK 给开发者集成。

---

## 0. 设计决策与边界

### 核心原则

1. **Schema-first**：每个 BgCapability 必须有 InputSchemaJSON 才能被投影为 Tool。
   无 schema 的能力不出现在 `/v1/bg/tools` 响应中（宁缺毋滥）。
2. **OpenAI Function Calling 兼容**：Tool 定义格式对齐 OpenAI `tools` 数组规范，
   确保 ChatGPT、Claude、LangChain、CrewAI 等框架零改造接入。
3. **Tool name 双向无损**：`bg.llm.chat.standard` ↔ `bg_llm_chat_standard`，
   点号和下划线互转，全小写，无信息损失。
4. **SDK ≠ 代码生成**：Python SDK 手写，保证人类可读的 API 设计和完整 type hints，
   不使用 OpenAPI generator。

### 本 Phase 不做

| 排除项 | 原因 | 何时做 |
|--------|------|--------|
| Tool 输入 JSON Schema 运行时校验 | 不引入额外 Go 依赖（无 jsonschema 库），留给 adapter Validate() | 后续 Phase |
| Adapter 侧 tool_call 输出解析 | 当前所有 adapter 只 emit `Type:"text"`，tool_call 输出需 LLM 循环路由，复杂度大 | Phase 18+ |
| TypeScript/Go SDK | Python 优先覆盖 Agent 生态；TS SDK 第二优先级 | Phase 17+ |
| SDK async/await (aiohttp) | 首版用 `httpx` 同步 + stream，async wrapper 后续补 | v2 |

---

## 1. Schema 填充 — 9 个种子能力补齐 InputSchemaJSON

### 现状

`model/bg_capability_seed.go` 的 `SeedBgCapabilities()` 创建了 9 个能力，但 `InputSchemaJSON` 和 `OutputSchemaJSON` 均为空。

### 方案

在 seed 中直接嵌入 JSON Schema 字符串（符合 JSON Schema Draft 2020-12）。
仅填充 InputSchemaJSON（Tool 投影必需）和 OutputSchemaJSON（文档用途）。

#### [MODIFY] model/bg_capability_seed.go — 每个能力补充 schema

| 能力 | InputSchemaJSON 核心字段 | OutputSchemaJSON 核心字段 |
|------|------------------------|-------------------------|
| `bg.llm.chat.fast` | `messages[]`, `temperature?`, `max_tokens?`, `tools?` | `choices[].message.content` |
| `bg.llm.chat.standard` | 同上 | 同上 |
| `bg.llm.chat.pro` | 同上 + `response_format?` | 同上 |
| `bg.llm.reasoning.pro` | `messages[]`, `reasoning_effort?` | `choices[].message.content`, `reasoning_content?` |
| `bg.video.generate.standard` | `prompt`, `duration?`, `aspect_ratio?` | `video_url`, `duration` |
| `bg.video.generate.pro` | 同上 + `style?`, `negative_prompt?` | 同上 |
| `bg.video.upscale.standard` | `video_url`, `target_resolution?` | `video_url`, `resolution` |
| `bg.sandbox.python` | `code`, `timeout_ms?` | `stdout`, `stderr`, `exit_code` |
| `bg.sandbox.session.standard` | `action`("execute"\|"upload"\|"download"), `code?`, `path?` | `stdout`, `stderr`, `files?` |

LLM 系列能力的 InputSchemaJSON 统一使用 OpenAI Messages 格式:

```json
{
  "type": "object",
  "properties": {
    "messages": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "role": { "type": "string", "enum": ["system", "user", "assistant"] },
          "content": { "type": "string" }
        },
        "required": ["role", "content"]
      },
      "minItems": 1,
      "description": "Conversation messages"
    },
    "temperature": { "type": "number", "minimum": 0, "maximum": 2, "description": "Sampling temperature" },
    "max_tokens": { "type": "integer", "minimum": 1, "description": "Maximum tokens to generate" }
  },
  "required": ["messages"]
}
```

#### [MODIFY] model/bg_capability_seed.go — SeedBgCapabilities 更新逻辑

现有 seed 使用 `FirstOrCreate`。为避免覆盖用户自定义 schema：
- seed 仅在 `InputSchemaJSON == ""` 时填充（条件更新）
- 新增 `updateSeedSchemas()` 函数，在 `SeedBgCapabilities()` 末尾调用

```go
func updateSeedSchemas() {
    schemas := map[string][2]string{
        "bg.llm.chat.fast":             {llmInputSchema, llmOutputSchema},
        "bg.llm.chat.standard":         {llmInputSchema, llmOutputSchema},
        // ...
    }
    for name, s := range schemas {
        DB.Model(&BgCapability{}).
            Where("capability_name = ? AND (input_schema_json IS NULL OR input_schema_json = '')", name).
            Updates(map[string]interface{}{
                "input_schema_json":  s[0],
                "output_schema_json": s[1],
            })
    }
}
```

Schema 字符串定义为包级常量（`const llmInputSchema = ...`），避免每次 seed 重复构建。

---

## 2. Tool 投影 — Capability → OpenAI Function Calling

### 2.1 DTO 定义

#### [MODIFY] dto/basegate.go — 新增 Tool 相关类型

```go
// ToolDefinition follows the OpenAI function calling tool format.
type ToolDefinition struct {
    Type     string              `json:"type"`     // always "function"
    Function ToolFunctionSchema  `json:"function"`
}

// ToolFunctionSchema describes a callable tool function.
type ToolFunctionSchema struct {
    Name        string      `json:"name"`                  // bg_llm_chat_standard
    Description string      `json:"description"`
    Parameters  interface{} `json:"parameters,omitempty"`   // parsed InputSchemaJSON
}

// ToolExecuteRequest is the body for POST /v1/bg/tools/execute.
type ToolExecuteRequest struct {
    Name      string                 `json:"name" binding:"required"` // bg_llm_chat_standard
    Arguments map[string]interface{} `json:"arguments"`               // tool call arguments
    Mode      string                 `json:"mode,omitempty"`          // sync | async | stream; default sync
    Metadata  map[string]string      `json:"metadata,omitempty"`
}

// ToolExecuteResponse wraps the BaseGateResponse with tool-specific context.
type ToolExecuteResponse struct {
    ToolCallID string            `json:"tool_call_id"`
    Name       string            `json:"name"`
    Response   BaseGateResponse  `json:"response"`
}
```

### 2.2 Tool 名称转换

#### [NEW] service/bg_tool_projection.go

```go
package service

// CapabilityNameToToolName converts dot-notation capability to underscore tool name.
//   "bg.llm.chat.standard" → "bg_llm_chat_standard"
func CapabilityNameToToolName(capName string) string {
    return strings.ReplaceAll(capName, ".", "_")
}

// ToolNameToCapabilityName converts underscore tool name back to dot-notation.
//   "bg_llm_chat_standard" → "bg.llm.chat.standard"
func ToolNameToCapabilityName(toolName string) string {
    return strings.ReplaceAll(toolName, "_", ".")
}

// ProjectCapabilitiesToTools converts active capabilities (with schema) into tool definitions.
func ProjectCapabilitiesToTools(caps []*model.BgCapability) []dto.ToolDefinition {
    var tools []dto.ToolDefinition
    for _, cap := range caps {
        if cap.InputSchemaJSON == "" {
            continue // 无 schema 的能力不投影
        }
        var params interface{}
        if err := common.Unmarshal([]byte(cap.InputSchemaJSON), &params); err != nil {
            continue // schema 解析失败跳过
        }

        desc := cap.Description
        if desc == "" {
            desc = fmt.Sprintf("%s %s (%s tier)", cap.Domain, cap.Action, cap.Tier)
        }
        // 追加模式和计费信息到描述
        desc += fmt.Sprintf(" [modes: %s, billing: %s]", cap.SupportedModes, cap.BillableUnit)

        tools = append(tools, dto.ToolDefinition{
            Type: "function",
            Function: dto.ToolFunctionSchema{
                Name:        CapabilityNameToToolName(cap.CapabilityName),
                Description: desc,
                Parameters:  params,
            },
        })
    }
    return tools
}
```

### 2.3 Controller

#### [NEW] controller/bg_tools.go

```go
package controller

// ListTools handles GET /v1/bg/tools
// Returns OpenAI-compatible tool definitions for all active capabilities with schema.
// Auth: TokenAuth (same as /v1/bg/responses)
func ListTools(c *gin.Context) {
    caps, err := model.GetActiveBgCapabilities()
    if err != nil {
        writeBGError(c, http.StatusInternalServerError, "internal_error", "failed to list capabilities")
        return
    }
    tools := service.ProjectCapabilitiesToTools(caps)
    c.JSON(http.StatusOK, gin.H{
        "object": "list",
        "data":   tools,
    })
}

// ExecuteTool handles POST /v1/bg/tools/execute
// Converts a tool call into a BaseGate response dispatch.
// Flow: parse tool name → resolve capability → build CanonicalRequest → dispatch
func ExecuteTool(c *gin.Context) {
    var req dto.ToolExecuteRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        writeBGError(c, http.StatusBadRequest, "invalid_request", err.Error())
        return
    }

    // Convert tool name → capability name
    capabilityName := service.ToolNameToCapabilityName(req.Name)

    // Build a BaseGateRequest and delegate to the standard dispatch flow
    mode := req.Mode
    if mode == "" {
        mode = "sync"
    }

    bgReq := &dto.BaseGateRequest{
        Model: capabilityName,
        Input: req.Arguments,
        ExecutionOptions: &dto.BGExecutionOptions{Mode: mode},
        Metadata: req.Metadata,
    }

    // Reuse the core dispatch logic (same as PostResponses, minus HTTP parsing)
    dispatchBaseGateRequest(c, bgReq)
}
```

`dispatchBaseGateRequest` 是从 `PostResponses` 中提取的共享调度逻辑（见 §2.4）。

### 2.4 提取共享调度逻辑

#### [REFACTOR] controller/bg_responses.go — 提取 dispatchBaseGateRequest

当前 `PostResponses()` 混合了 HTTP 解析和调度逻辑。提取核心调度为独立函数：

```go
// dispatchBaseGateRequest is the shared dispatch logic used by both
// PostResponses (HTTP JSON body) and ExecuteTool (tool call conversion).
func dispatchBaseGateRequest(c *gin.Context, req *dto.BaseGateRequest) {
    // 1. resolveProjectID
    // 2. build CanonicalRequest
    // 3. idempotency key
    // 4. billing context
    // 5. policy check
    // 6. mode dispatch (sync/async/stream)
    // 7. response
}
```

`PostResponses` 变为：parse JSON body → call `dispatchBaseGateRequest(c, &req)`。

### 2.5 路由注册

#### [MODIFY] router/relay-router.go

在现有 BaseGate Session routes 之后（约 line 128）新增：

```go
// BaseGate Tool routes (Phase 15)
httpRouter.GET("/bg/tools", controller.ListTools)
httpRouter.POST("/bg/tools/execute", controller.ExecuteTool)
```

使用相同的 `TokenAuth()` + `Distribute()` 中间件链。

---

## 3. Tool 执行端到端验证

### 3.1 名称转换测试

#### [NEW] service/bg_tool_projection_test.go

| 测试 | 输入 | 预期输出 |
|------|------|----------|
| Cap→Tool 正向 | `bg.llm.chat.standard` | `bg_llm_chat_standard` |
| Tool→Cap 反向 | `bg_llm_chat_standard` | `bg.llm.chat.standard` |
| 往返无损 | `bg.video.generate.pro` → tool → cap | 原值 |
| 空字符串 | `""` | `""` |

### 3.2 投影测试

#### [NEW] service/bg_tool_projection_test.go（续）

| 测试 | 场景 | 预期 |
|------|------|------|
| 有 schema 的能力 | InputSchemaJSON 非空 | 出现在 tools 列表 |
| 无 schema 的能力 | InputSchemaJSON 为空 | **不**出现在 tools 列表 |
| schema 解析失败 | InputSchemaJSON = `{invalid` | 跳过，不报错 |
| 描述拼接 | Description + modes + billing | 完整描述字符串 |

### 3.3 Controller 测试

#### [NEW] controller/bg_tools_test.go

| 测试 | 场景 | 预期 |
|------|------|------|
| GET /v1/bg/tools 正常 | seed 能力有 schema | 返回 tools 数组 |
| GET /v1/bg/tools 空 | 无 active 能力 | 返回空数组 |
| POST /v1/bg/tools/execute sync | name=bg_llm_chat_standard | 返回 200 + response |
| POST /v1/bg/tools/execute 不存在的 tool | name=bg_nonexistent | adapter not found error |
| POST /v1/bg/tools/execute 无 name | body 缺 name | 400 |

---

## 4. Python SDK

### 4.1 目录结构

```
sdk/python/
├── basegate/
│   ├── __init__.py          # 导出 BaseGate 主类
│   ├── _client.py           # HTTP 客户端封装
│   ├── _exceptions.py       # 异常类层级
│   ├── _types.py            # Pydantic/dataclass 类型定义
│   ├── _sse.py              # SSE 流解析器
│   ├── resources/
│   │   ├── __init__.py
│   │   ├── responses.py     # bg.responses.create/get/cancel/stream/poll
│   │   ├── sessions.py      # bg.sessions.create/execute/close/get
│   │   └── tools.py         # bg.tools.list/execute
│   └── _version.py          # 版本号
├── tests/
│   ├── test_client.py
│   ├── test_responses.py
│   ├── test_sessions.py
│   ├── test_tools.py
│   └── test_sse.py
├── pyproject.toml            # PEP 621 包配置
├── README.md
└── LICENSE
```

### 4.2 核心 API 设计

```python
from basegate import BaseGate

bg = BaseGate(
    api_key="sk-xxx",
    base_url="http://localhost:3000",  # 默认
    timeout=30.0,                       # 秒
    max_retries=2,                      # 自动重试 5xx/429
)

# ─── Responses ────────────────────────────────────────────

# Sync
result = bg.responses.create(
    model="bg.llm.chat.standard",
    input={"messages": [{"role": "user", "content": "Hello!"}]},
)
print(result.output[0].content)

# Stream
for event in bg.responses.stream(
    model="bg.llm.chat.standard",
    input={"messages": [{"role": "user", "content": "Hello!"}]},
):
    if event.type == "response.output_text.delta":
        print(event.delta, end="", flush=True)

# Async
resp = bg.responses.create(
    model="bg.video.generate.standard",
    input={"prompt": "A sunset over mountains"},
    mode="async",
)
# Poll until complete
result = bg.responses.poll(resp.id, interval=2.0, timeout=120.0)

# Get by ID
result = bg.responses.get("resp_xxxx")

# Cancel
bg.responses.cancel("resp_xxxx")

# ─── Sessions ─────────────────────────────────────────────

session = bg.sessions.create(model="bg.sandbox.session.standard")
action = bg.sessions.execute(session.id, action="execute", input={"code": "print(42)"})
print(action.output)
bg.sessions.close(session.id)

# ─── Tools ────────────────────────────────────────────────

tools = bg.tools.list()  # → [ToolDefinition, ...]
result = bg.tools.execute(name="bg_video_generate_standard", arguments={"prompt": "..."})
```

### 4.3 类型定义

```python
# basegate/_types.py
from dataclasses import dataclass, field
from typing import Any, Optional

@dataclass
class Response:
    id: str
    object: str
    created_at: int
    status: str
    model: str
    output: list["OutputItem"] = field(default_factory=list)
    usage: Optional["Usage"] = None
    pricing: Optional["Pricing"] = None
    error: Optional["Error"] = None
    poll_url: Optional[str] = None

@dataclass
class OutputItem:
    type: str       # text | image | video | tool_call | ...
    content: Any
    role: Optional[str] = None

@dataclass
class Usage:
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0

@dataclass
class StreamEvent:
    event: str      # response.output_text.delta | response.completed | ...
    data: dict
    delta: Optional[str] = None  # 便捷字段，仅 delta 事件有值

@dataclass
class ToolDefinition:
    type: str
    function: "FunctionSchema"

@dataclass
class FunctionSchema:
    name: str
    description: str
    parameters: Optional[dict] = None

@dataclass
class Session:
    id: str
    status: str
    model: str

@dataclass
class SessionAction:
    action_id: str
    status: str
    output: Any = None
```

### 4.4 HTTP 客户端实现要点

| 要点 | 实现 |
|------|------|
| HTTP 库 | `httpx`（同步模式），支持 timeout + retry |
| 认证 | `Authorization: Bearer {api_key}` 自动注入 |
| 重试 | 429/500/502/503 → 指数退避，最多 `max_retries` 次 |
| 错误映射 | HTTP 4xx/5xx → `BaseGateError` 子类（`AuthenticationError`, `RateLimitError`, `APIError`）|
| Stream | `httpx.stream("POST", ...)` → 行级 SSE 解析 → yield `StreamEvent` |
| Project 头 | 可选 `project_id` → `X-Project-Id` header |
| User-Agent | `basegate-python/{version}` |

### 4.5 SSE 解析器

```python
# basegate/_sse.py
def iter_sse_events(response):
    """Parse SSE stream from httpx response, yield (event_type, data_str)."""
    buffer = ""
    for chunk in response.iter_text():
        buffer += chunk
        while "\n\n" in buffer:
            block, buffer = buffer.split("\n\n", 1)
            event_type, data = "", ""
            for line in block.split("\n"):
                if line.startswith("event: "):
                    event_type = line[7:].strip()
                elif line.startswith("data: "):
                    data = line[6:].strip()
            if data:
                yield event_type, data
```

### 4.6 SDK 测试策略

| 测试 | 方式 | 说明 |
|------|------|------|
| 类型构造 | 单测 | Response/StreamEvent dataclass 序列化/反序列化 |
| HTTP 客户端 | pytest + respx | mock httpx 请求，验证 header/body/retry |
| SSE 解析 | 单测 | 模拟 SSE 字节流，验证事件解析 |
| 集成测试 | 可选 | 需要运行中的 BaseGate 实例，标记 `@pytest.mark.integration` |

### 4.7 pyproject.toml

```toml
[project]
name = "basegate"
version = "0.1.0"
description = "Python SDK for BaseGate — Unified AI Capability Gateway"
requires-python = ">=3.8"
dependencies = ["httpx>=0.24"]
license = { text = "MIT" }

[project.optional-dependencies]
dev = ["pytest", "respx", "pytest-cov"]
```

---

## 5. 执行顺序

| Step | 内容 | 估时 | 依赖 |
|------|------|------|------|
| **S1** | `model/bg_capability_seed.go` — 9 个能力补充 InputSchemaJSON/OutputSchemaJSON | 1.5h | — |
| **S2** | `dto/basegate.go` — 新增 ToolDefinition, ToolExecuteRequest, ToolExecuteResponse | 0.5h | — |
| **S3** | `service/bg_tool_projection.go` — 名称转换 + 投影逻辑 | 1h | S1, S2 |
| **S4** | `service/bg_tool_projection_test.go` — 名称转换 + 投影单测 | 1h | S3 |
| **S5** | `controller/bg_responses.go` — 提取 `dispatchBaseGateRequest` 共享逻辑 | 1.5h | — |
| **S6** | `controller/bg_tools.go` — ListTools + ExecuteTool handlers | 1.5h | S3, S5 |
| **S7** | `router/relay-router.go` — 注册 `/v1/bg/tools` 和 `/v1/bg/tools/execute` | 0.5h | S6 |
| **S8** | `controller/bg_tools_test.go` — controller 测试 | 1.5h | S6, S7 |
| **S9** | `go build` + `go test` — 后端全绿 | 0.5h | S1-S8 |
| **S10** | `sdk/python/basegate/` — 核心模块（_client, _types, _exceptions, _sse） | 3h | — |
| **S11** | `sdk/python/basegate/resources/` — responses, sessions, tools | 3h | S10 |
| **S12** | `sdk/python/tests/` — 单测（mock HTTP + SSE） | 2h | S10, S11 |
| **S13** | `sdk/python/pyproject.toml` + `README.md` | 0.5h | S10 |

**总计约 ~18h（2.5-3d 实际工作时间）**

后端（S1-S9）：~9.5h — 可独立交付
SDK（S10-S13）：~8.5h — 可并行或串行

---

## 6. 验收标准

### 后端

- [ ] `GET /v1/bg/tools` 返回所有有 schema 的 active capability 的 tool definition
- [ ] 返回格式兼容 OpenAI function calling `tools` 数组（type=function, function.name/description/parameters）
- [ ] Tool name ↔ capability name 双向转换无损（`bg.llm.chat.standard` ↔ `bg_llm_chat_standard`）
- [ ] 无 InputSchemaJSON 的能力不出现在 `/v1/bg/tools`
- [ ] `POST /v1/bg/tools/execute` 可成功调度 sync 模式请求
- [ ] `POST /v1/bg/tools/execute` 支持 mode=async 异步调度
- [ ] Tool execute 复用 PostResponses 的完整链路（policy check、billing、audit）
- [ ] 9 个种子能力全部有 InputSchemaJSON（SeedBgCapabilities 更新后）
- [ ] `go build ./...` 编译通过
- [ ] `go test ./service/ ./controller/` Phase 15 相关测试全绿

### Python SDK

- [ ] `pip install ./sdk/python` 可安装
- [ ] `bg.responses.create()` sync 模式正常
- [ ] `bg.responses.stream()` 返回 SSE event iterator
- [ ] `bg.responses.create(mode="async")` + `bg.responses.poll()` 异步模式正常
- [ ] `bg.sessions.create/execute/close` session 生命周期正常
- [ ] `bg.tools.list()` 返回 ToolDefinition 列表
- [ ] `bg.tools.execute()` 调用成功
- [ ] 所有公开方法有 type hints
- [ ] `pytest sdk/python/tests/` 全绿

---

## 7. 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `model/bg_capability_seed.go` | MODIFY | 9 个能力补充 InputSchemaJSON + OutputSchemaJSON |
| `dto/basegate.go` | MODIFY | 新增 ToolDefinition, ToolExecuteRequest, ToolExecuteResponse |
| `service/bg_tool_projection.go` | NEW | 名称转换 + 投影逻辑 |
| `service/bg_tool_projection_test.go` | NEW | 投影单测（名称转换、schema 过滤、描述拼接）|
| `controller/bg_responses.go` | MODIFY | 提取 dispatchBaseGateRequest 共享调度 |
| `controller/bg_tools.go` | NEW | ListTools + ExecuteTool handlers |
| `controller/bg_tools_test.go` | NEW | tools controller 测试 |
| `router/relay-router.go` | MODIFY | 注册 /v1/bg/tools 路由 |
| `sdk/python/basegate/__init__.py` | NEW | 包入口 + BaseGate 类导出 |
| `sdk/python/basegate/_client.py` | NEW | HTTP 客户端 |
| `sdk/python/basegate/_types.py` | NEW | 类型定义 |
| `sdk/python/basegate/_exceptions.py` | NEW | 异常层级 |
| `sdk/python/basegate/_sse.py` | NEW | SSE 解析器 |
| `sdk/python/basegate/_version.py` | NEW | 版本号 |
| `sdk/python/basegate/resources/responses.py` | NEW | Responses 资源 |
| `sdk/python/basegate/resources/sessions.py` | NEW | Sessions 资源 |
| `sdk/python/basegate/resources/tools.py` | NEW | Tools 资源 |
| `sdk/python/tests/` | NEW | pytest 测试套件 |
| `sdk/python/pyproject.toml` | NEW | 包配置 |
| `sdk/python/README.md` | NEW | SDK 使用文档 |
