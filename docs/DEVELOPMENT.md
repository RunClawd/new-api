# BaseGate / New API — 本地开发指南

## 前提条件

| 工具 | 最低版本 | 安装方式 |
|------|---------|---------|
| **Go** | 1.23+ (go.mod 要求 1.25.1，自动下载) | `brew install go` |
| **Bun** | 1.x | `curl -fsSL https://bun.sh/install \| bash` |
| **Node** | 18+ (仅 Bun fallback) | `nvm install 18` |
| **Git** | 2.x | `brew install git` |

> [!NOTE]
> go.mod 声明了 `go 1.25.1`，但你本地只需 Go 1.23+。
> 运行时设置 `GOTOOLCHAIN=auto`，Go 会自动下载 1.25.1 工具链。
> 建议写入 shell 配置：`echo 'export GOTOOLCHAIN=auto' >> ~/.zshrc`

---

## 1. 克隆与初始化

```bash
# 克隆仓库
git clone https://github.com/QuantumNous/new-api.git
cd new-api

# 下载 Go 依赖（首次会自动下载 Go 1.25.1 toolchain）
GOTOOLCHAIN=auto go mod download

# 安装前端依赖
cd web && bun install && cd ..
```

---

## 2. 运行测试

### 全量测试（推荐，TDD 开发时频繁执行）

```bash
# 运行全部后端测试（使用内存 SQLite，无需外部数据库）
GOTOOLCHAIN=auto go test ./model/... ./dto/... ./service/... -count=1
```

### 按模块运行

```bash
# 模型层测试（7 张 bg_ 表 + CAS + 状态机逻辑）
GOTOOLCHAIN=auto go test ./model/... -v -run "TestBg" -count=1

# DTO 序列化测试
GOTOOLCHAIN=auto go test ./dto/... -v -run "TestBase" -count=1

# 状态机测试
GOTOOLCHAIN=auto go test ./service/... -v -run "TestDeriveResponseStatus|TestValidateTransition|TestMapProviderEvent|TestApplyProviderEvent" -count=1

# 现有 Task 计费测试（确认无回归）
GOTOOLCHAIN=auto go test ./service/... -v -run "TestRefundTaskQuota|TestRecalculate|TestCAS" -count=1
```

### 运行单个测试

```bash
GOTOOLCHAIN=auto go test ./model/... -v -run "TestBgResponse_CASUpdateStatus_ConcurrentWinner" -count=1
```

### 竞态检测（CI 必跑）

```bash
GOTOOLCHAIN=auto go test -race ./model/... ./service/... -count=1
```

---

## 3. 本地启动（开发模式）

### 方式一：最小化后端（SQLite，无前端）

```bash
# 在项目根目录
GOTOOLCHAIN=auto go run main.go
```

默认监听 `http://localhost:3000`，使用 SQLite（自动创建 `./new-api.db`）。
首次启动自动创建 root 用户：`username: root / password: 123456`

### 方式二：前端 + 后端分离开发

**终端 1 — 后端：**
```bash
GOTOOLCHAIN=auto go run main.go
```

**终端 2 — 前端（热重载）：**
```bash
cd web
bun run dev
```

前端开发服务器默认跑在 `http://localhost:5173`，自动代理 API 请求到后端 `:3000`。

### 方式三：Makefile 一键启动

```bash
make all
```

等效于：先 `bun run build` 构建前端静态文件 → 再 `go run main.go` 启动后端（前端嵌入到 Go 二进制中）。

---

## 4. 构建生产二进制

```bash
# 构建前端
cd web && bun install && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat ../VERSION) bun run build && cd ..

# 构建后端（嵌入前端静态文件）
GOTOOLCHAIN=auto CGO_ENABLED=0 go build \
  -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" \
  -o new-api

# 运行
./new-api
```

### Docker 构建

```bash
docker build -t basegate/new-api:dev .
docker run -p 3000:3000 -v ./data:/data basegate/new-api:dev
```

---

## 5. 数据库配置

### 开发（默认 SQLite）

无需任何配置，直接运行即可。数据库文件自动创建在工作目录。

### MySQL

```bash
export SQL_DSN="root:password@tcp(127.0.0.1:3306)/basegate?parseTime=true"
GOTOOLCHAIN=auto go run main.go
```

### PostgreSQL

```bash
export SQL_DSN="postgres://user:password@localhost:5432/basegate?sslmode=disable"
GOTOOLCHAIN=auto go run main.go
```

> [!TIP]
> 所有 7 张 `bg_` 表在启动时自动创建（AutoMigrate），无需手动建表。

---

## 6. 环境变量速查

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 监听端口 | `3000` |
| `SQL_DSN` | 数据库 DSN | 空（使用 SQLite） |
| `LOG_SQL_DSN` | 日志数据库 DSN | 空（同主库） |
| `REDIS_CONN_STRING` | Redis 连接串 | 空（禁用缓存） |
| `DEBUG` | 调试模式（打印 SQL） | `false` |
| `SESSION_SECRET` | Session 密钥 | 随机 |
| `GOTOOLCHAIN` | Go 工具链自动下载 | 需手动设 `auto` |

完整变量列表见 [.env.example](../.env.example)

---

## 7. 项目结构（BaseGate 相关）

> 完整的 BaseGate 架构文档见 [docs/BASEGATE.md](BASEGATE.md)。

```text
new-api/
├── model/
│   ├── bg_response.go         # Response 表 + 状态机 + CAS
│   ├── bg_attempt.go          # Attempt 表 + CAS + pollable 查询
│   ├── bg_billing.go          # Usage + Billing + Ledger 表
│   ├── bg_session.go          # Session + SessionAction 表 + CAS lock
│   ├── bg_webhook.go          # Webhook events 表 + 状态常量
│   ├── bg_capability.go       # Capability contract 表
│   ├── bg_audit.go            # 审计日志（异步写入）
│   ├── bg_response_test.go    # Model 层测试
│   └── main.go                # AutoMigrate 注册（10 张 bg_ 表）
│
├── dto/
│   ├── basegate.go            # API DTO（Request/Response/Session/Model/Usage/Error）
│   └── basegate_test.go       # 序列化测试
│
├── controller/
│   ├── bg_responses.go        # POST/GET /v1/bg/responses, POST cancel
│   ├── bg_responses_test.go   # Controller 集成测试
│   ├── bg_sessions.go         # POST/GET /v1/bg/sessions, POST action/close
│   ├── bg_sessions_test.go    # Session controller 测试
│   └── model.go               # /v1/models BaseGate capability 注入
│
├── service/
│   ├── bg_orchestrator.go     # DispatchSync / DispatchAsync（含 fallback）
│   ├── bg_state_machine.go    # ApplyProviderEvent + auto-advance + billing 触发
│   ├── bg_streaming.go        # DispatchStream（SSE 生命周期）
│   ├── bg_billing_engine.go   # FinalizeBilling（事务） + LookupPricing
│   ├── bg_session_manager.go  # CreateSession / ExecuteAction / CloseSession
│   ├── bg_session_worker.go   # Idle/expire 超时执行
│   ├── bg_poll_worker.go      # 异步任务轮询
│   ├── bg_outbox.go           # EnqueueWebhookEvent
│   ├── bg_webhook_worker.go   # Webhook 投递 + 重试 + 退避
│   └── *_test.go              # 各模块单元/集成测试
│
├── relay/
│   ├── basegate/
│   │   ├── provider_adapter.go     # ProviderAdapter + SessionCapableAdapter 接口
│   │   ├── adapter_registry.go     # 1:N registry + LookupAdapters/ByName
│   │   ├── legacy_wrapper.go       # 桥接现有 TaskAdaptors
│   │   └── adapters/
│   │       └── openai_llm_adapter.go  # Native OpenAI adapter（raw HTTP）
│   ├── bg_register.go              # Adapter 注册入口
│   └── common/
│       ├── canonical.go            # CanonicalRequest / AdapterResult / ID 生成器
│       └── sse_event.go            # SSE 事件类型定义
│
└── router/
    └── relay-router.go        # 7 条 BaseGate 路由
```

---

## 8. 开发工作流

### TDD 循环

```text
1. 写测试 → 2. 运行测试（红色） → 3. 写实现 → 4. 运行测试（绿色） → 5. 重构
```

### 提交前检查清单

```bash
# 1. 全量测试通过
GOTOOLCHAIN=auto go test ./model/... ./dto/... ./service/... -count=1

# 2. 竞态检测
GOTOOLCHAIN=auto go test -race ./model/... ./service/... -count=1

# 3. 编译检查（确认无语法/导入问题）
GOTOOLCHAIN=auto go build ./...

# 4. vet 静态分析
GOTOOLCHAIN=auto go vet ./...
```

---

## 9. 常见问题

### Q: `go.mod requires go >= 1.25.1 (running go 1.23.3; GOTOOLCHAIN=local)`

**A:** 设置 `GOTOOLCHAIN=auto` 即可。Go 会自动下载 1.25.1 工具链到 `~/sdk/go1.25.1/`。  
建议永久写入：
```bash
echo 'export GOTOOLCHAIN=auto' >> ~/.zshrc && source ~/.zshrc
```

### Q: 测试跑不过，找不到 `bg_responses` 表？

**A:** 确认 `task_cas_test.go` 中的 `TestMain` 已包含 `&BgResponse{}` 等 AutoMigrate 注册。  
如果不确定，执行 `git diff model/task_cas_test.go` 检查。

### Q: 前端构建失败？

**A:** 确认 bun 已安装且版本 >= 1.0：
```bash
bun --version
cd web && bun install && bun run build
```

### Q: IDE lint 报错 "packages.Load error"？

**A:** 这是 IDE 使用本地 Go 版本（1.23.3）而非 GOTOOLCHAIN=auto 导致。不影响实际编译和测试。  
如需 IDE 支持，安装 Go 1.25.1 作为系统默认版本：
```bash
go install golang.org/dl/go1.25.1@latest
go1.25.1 download
# 然后将 IDE 的 Go SDK 指向 ~/sdk/go1.25.1
```
