# P0 平台内核技术方案

本包建立后面所有工作包可以同时编译、同时写测试的骨架。它不实现资源发布、搜索、登录业务和采集。

## 功能点

- `server/` Go module，`cmd/api`、`cmd/worker`、`cmd/nexadm` 三个入口。
- 配置从环境变量读取，缺必填项时进程以非 0 退出并打印缺哪一项。
- PostgreSQL 连接池、goose 迁移、第一阶段 15 张表。
- River 官方迁移与 worker 进程。worker 先只注册一个空的 `ping` 任务，用来证明事务入队。
- 统一错误、请求 ID、健康检查、slog 日志。
- `api/openapi.yaml` 放入 [总契约](00-split-and-contracts.md) 的组件和路径骨架。路径可以先返回 501，`code` 为 `not_implemented`。
- `deploy/compose.yaml` 只起 PostgreSQL 17。API 与 worker 的服务定义留注释，等对应包有二进制再打开。
- CI：`go test ./...`、`sqlc diff`、对测试库执行迁移。测试不拉取模型、不访问 GitHub。

## 版本锁定

M0 合并当天把准确补丁写入 `go.mod` 和 compose，之后升级单独开 PR。

| 部件 | 锁定目标 |
| --- | --- |
| Go | 1.25 的当前补丁 |
| PostgreSQL | 17 |
| 访问库 | `github.com/jackc/pgx/v5` |
| SQL 生成 | sqlc 1.31 的当前补丁 |
| 迁移 | goose v3 |
| 队列 | River 开源版，应用代码与它的迁移同一 PR 锁定 |
| 日志 | 标准库 `log/slog` |
| HTTP | 标准库 `net/http` ServeMux |

不引入 Gin、Echo、GORM、Redis。

## 目录

```text
server/
  cmd/api/main.go
  cmd/worker/main.go
  cmd/nexadm/main.go
  internal/platform/config/config.go
  internal/platform/apperr/error.go
  internal/platform/httpx/middleware.go
  internal/platform/clock/clock.go
  internal/platform/id/id.go
  internal/store/db.go
  internal/store/sqlc/          # 生成
  internal/ports/ports.go
  internal/jobs/river.go
  db/migrations/
  db/queries/
api/openapi.yaml
deploy/compose.yaml
.env.example
```

`.env.example` 只有键名和说明，没有口令。`.gitignore` 忽略 `.env`、备份和 `server/bin/`。

## 配置

`config.Load()` 返回结构体，不在业务里读 `os.Getenv`。

| 变量 | 必填 | 含义 |
| --- | --- | --- |
| `NEX_DATABASE_URL` | 是 | pgx URL |
| `NEX_HTTP_ADDR` | 否，默认 `:8080` | API 监听 |
| `NEX_PUBLIC_BASE_URL` | 是 | 用于 Cookie 的 Secure 判断和日志 |
| `NEX_SESSION_SECRET` | 是 | 至少 32 字节，用于访客 HMAC |
| `NEX_CURSOR_SECRET` | 是 | base64 编码，解码后至少 32 个随机字节，独立用于游标签名 |
| `NEX_CURSOR_KEY_ID` | 否，默认 k1 | 当前签名密钥标识，轮换密钥时必须更换 |
| `NEX_CURSOR_PREVIOUS_KEYS` | 否，默认空映射 | 旧 key ID 到 base64 密钥的 JSON 映射，仅用于验签 |
| `NEX_ENVIRONMENT` | 是 | `development` `test` `production` |
| `NEX_PUBLIC_INCLUDE_DEMO` | 否 | production 默认 false，development 默认 true |
| `NEX_MODEL_ENABLED` | 否，默认 false | 开启真实调用；false 默认绑定 DisabledClient |
| `NEX_MODEL_FIXTURE` | 否，默认 false | 仅 development/test 显式使用假模型；生产禁止 |

生产环境如果 `NEX_PUBLIC_INCLUDE_DEMO=true`，启动直接失败。这个组合会把示例内容暴露到线上。

生产环境 `NEX_MODEL_FIXTURE=true`，或模型两个开关同时为 true，启动失败。默认关闭模型时返回 `model_disabled`，不得把夹具结果写进生产加工链。

CursorSigner 只从独立游标密钥配置构造，不从 NEX_SESSION_SECRET 派生。当前 key ID 不得与旧映射重复，所有密钥校验长度且不进入日志。签名使用 HMAC-SHA256 和 cursor.v1 域前缀，验签用 hmac.Equal 恒定时间比较。轮换时先部署新旧验签集合，再切换签名 key ID；所有实例停止旧密钥签名后至少保留旧验签密钥 24 小时。紧急撤销可以立即移除旧密钥，此时旧游标明确返回 cursor_stale。

## 设计对象与模式

### Clock 与 ID 生成器

```go
type Clock interface {
    Now() time.Time
}

type IDGenerator interface {
    New() uuid.UUID
}
```

生产用 `time.Now().UTC()` 和 `uuid.New()`。测试注入固定时钟和递增 UUID。这是**依赖倒置**：领域服务不调用 `time.Now`，排名衰减和会话过期才有可重复测试。

ID 策略：业务实体用 UUIDv4，由 Go 生成后插入。审计表用 bigint identity，不经过这个接口。

### 错误值

`apperr.Error` 含 `Code`、`Message`、`HTTPStatus`、`Fields`、`Op`。用 `errors.Is` / `errors.As` 判断，不用字符串比较。预定义哨兵：

| Code | HTTP | 场景 |
| --- | --- | --- |
| `invalid_argument` | 400 | 字段不合法 |
| `unauthenticated` | 401 | 没有会话 |
| `forbidden` | 403 | CSRF 或来源不对 |
| `not_found` | 404 | 未发布也用这个，不暴露草稿是否存在 |
| `edit_conflict` | 409 | edit_version 不符 |
| `draft_conflict` | 409 | 已有未发布草稿，不能应用采集建议 |
| `idempotency_mismatch` | 409 | 同键不同请求 |
| `request_busy` | 503 | 等锁超过上限，使用原键和原请求稍后重试 |
| `cursor_stale` | 400 | 游标条件、签名或有效期不符 |
| `rate_limited` | 429 | 登录或事件过频 |
| `not_implemented` | 501 | 骨架路径 |
| `unavailable` | 503 | 数据库不可用 |

`httpx.WriteError` 是唯一把错误写成 JSON 的地方。处理器返回 `error`，不自己写状态码。

### 工作单元

```go
func Within(ctx context.Context, pool *pgxpool.Pool, fn func(ctx context.Context, tx pgx.Tx) error) error
```

`fn` 返回错误就回滚。River 入队使用 `river.InsertTx`，和业务插入同一 `tx`。P0 的验收测试：在 `fn` 里插入一行并入队，然后返回错误，提交后行和任务都不存在。

这是**工作单元**，只由最外层写命令或幂等执行器调用一次。P1 的 `CreateDraftTx`、`SaveDraftTx`、`PublishTx`、P7 的 `DecideTx` 和所有仓储显式接收该 `pgx.Tx`，不能再次调用 Within 或自行 Begin/Commit。调用链中不通过 context 自动创建、替换或隐藏事务。

幂等响应先在事务内构造并持久化，提交成功后才写 HTTP。技术错误回滚全部变更；需要持久化的业务决定（如将建议标记 conflict）通过带 HTTP 状态的结果返回，由外层提交后再返回 409，不能一边要求保存冲突状态一边返回触发回滚的错误。

直接保存或发布的版本冲突返回 error 并回滚；建议审核发现过期或人工草稿时，可以返回 409 的 WriteResult 以仅提交审核决定。完整区分见总契约，状态码不决定事务行为。短查询及管理等锁限制均使用 SET LOCAL，并在同一个显式事务中执行后续语句；所有错误和取消路径先回滚，再将连接归还池。

### HTTP 中间件链

顺序固定：`Recover` → `RequestID` → `AccessLog` → 路由。Request ID 优先采用入站 `X-Request-ID`（限制 64 字符、可见 ASCII），否则用 UUIDv7 或 UUIDv4。响应回写同一个头。panic 转成 500，`code` 为 `internal`，日志带栈，响应不带栈。

路由使用 Go 1.22 方法模式：`GET /api/v1/resources/{id}`。API 未匹配返回 JSON 404。页面路由以后由 P4 的静态资源处理，P0 不把未知路径回成 `index.html`。

### 健康检查

`GET /health/live` 不碰数据库，200 `{ "status": "live" }`。`GET /health/ready` 执行 `select 1`，失败 503 `{ "status": "unavailable" }`。两者都不返回版本以外的配置。可以加 `version`，值为编译时 `-ldflags` 注入的提交号。

## 迁移

工具是 goose。目录 `server/db/migrations`，文件名 `00001_phase1_identity.sql` 这种顺序号。

P0 一次性写入实施方案里的两批第一阶段表：

1. `admin_users`、`admin_sessions`、`audit_logs`、`idempotency_requests`、`resources`、`resource_revisions`、`tags`、`tag_aliases`、`resource_publications`、`resource_tags`。
2. `featured_slots`、`interaction_events`、`resource_metrics_daily`、`ranking_runs`、`ranking_entries`。

字段、外键、部分唯一索引、GiST 排斥约束按 [数据库表结构](../database-schema.md) 翻译。另外加入 P1 声明的 `resources.freshness_eligible`。扩展：`pgcrypto`（仅当需要 gen_random_uuid 作为兜底；主键仍由 Go 生成）、`pg_trgm`、`btree_gist`。

River 使用它自己的 `river.Migrate`，不把 River 表抄进 goose。启动顺序：goose → River migrate → 监听。

回滚文件只为尚未上线的迁移保留。已经在共享库执行过的 up 不修改。

第一阶段验收后，P0 续作第二阶段 schema 基线 PR。依次创建：信源及运行、凭据和资料版本表；processing_runs；change_proposals 与 resource_evidence；provider_calls、预算窗口与预占表；external_metric_snapshots。循环指针在目标表建成后补外键。包括 P7 的 pipeline_key、pipeline_plan 等已同步到数据库设计的字段，合计仍为 13 张表。P6/P7/P8 不重复提交 CREATE TABLE，基线空库迁移通过后才独立合并各实现。

同一第二阶段基线 ALTER 第一阶段已有的 idempotency_requests，增加 parent_id、item_index、request_meta 及父子约束。第一阶段管理行用空 parent_id/item_index 和空元信息兼容；这属于已有表增量，不增加应用表数量。

## OpenAPI 骨架

`api/openapi.yaml` 使用 OpenAPI 3.1。P0 写入公共组件：`Error`、`ResourceCard`、`ResourceDetail`、`TagCount`、`ResourcePage`、`FeaturedPage`、`EventBatch`，以及本文上面的枚举。路径先全部列出，`operationId` 稳定：

| operationId | 归属 |
| --- | --- |
| `listResources` `getResource` `getResourceBySlug` `listTags` `listFeatured` `postEvents` | P3 |
| `login` `logout` `currentAdmin` 以及 `/api/admin/resources`、`/tags`、`/featured` | P1 与 P2 按路径分 |
| `live` `ready` | P0 |

P0 的处理器返回 501。后续 PR 替换实现时保持 `operationId` 和组件名不变。前端用 `openapi-typescript` 生成类型，不生成服务端代码。

## 可观测性

每个请求一行 slog JSON：`request_id`、`method`、`path`（不带查询串）、`status`、`duration_ms`。查询串可能含搜索词，不进 info 日志。数据库错误打在 error，对外仍是 `unavailable` 或 `internal`。

## 测试

- 配置缺 `NEX_DATABASE_URL` 时 `Load` 返回列出该项的错误。
- `Within` 回滚测试，使用真实 PostgreSQL。
- 入队后回滚，`river_job` 里没有该任务。
- ready 在数据库关闭时返回 503，live 仍是 200。
- 未知 `/api/` 路径是 JSON，不是 HTML。
- 生产 fixture 配置被拒绝；默认 DisabledClient 不返回加工文本。
- 游标缺密钥或密钥过短启动失败；访客密钥轮换不改变游标验签结果，旧游标在旧验签密钥保留期内可用。
- 连接池复用同一连接，查询成功、超时、取消后 statement_timeout 与 lock_timeout 都恢复事务前设置。
- P2 外层事务调用 P1 Tx 方法并人为失败，资源、投影、审计、幂等行和任务同时回滚；整个操作只获取一条事务连接。
- 第二阶段基线在仅有第一阶段表的空业务库上完整迁移，所有外键目标存在。

## 完成定义

空库执行 `nexadm migrate` 后，15 张应用表和 River 表存在。`api` 与 `worker` 能同时连上该库。其他工作包可以在此之上加文件而不改 `main` 的启动顺序。启动顺序封装成 `app.Run(ctx, config)`，新路由用 `httpapi.Mount(mux)` 注册，避免每个包改一遍 `main`。
