# P2 管理认证技术方案

本包负责管理员是谁、请求有没有被改过、重复提交如何复用结果、审计能不能被改掉。它不实现资源字段校验，那是 P1。

归属：`internal/adminauth`、`db/queries/auth.sql`。实现 `ports.Auditor`。向 HTTP 层提供中间件。

## 功能点

- 单管理员口令登录、登出、读取当前会话。
- 会话 Cookie 与 CSRF。
- 登录失败限流。
- 写请求的幂等记录，和业务事务一起提交。
- 审计只插入，不提供更新接口。
- `nexadm admin init` 创建第一个账号。

不做：多角色、OAuth、用户注册、采集凭据。采集凭据在 P6。

## 领域对象

```go
type Admin struct {
    id           AdminID
    username     Username
    passwordHash string
    status       AccountStatus // active, disabled
}

type Session struct {
    id              SessionID
    adminID         AdminID
    tokenHash       []byte
    csrfSecretHash  []byte
    expiresAt       time.Time
    revokedAt       *time.Time
}

type IdempotencyRecord struct {
    principalKey   string // "admin:<uuid>"
    scope          string
    key            string
    requestHash    []byte
    status         IdemStatus // processing, completed
    responseStatus int
    responseBody   []byte
}
```

`Username`：NFKC、去掉首尾空白、小写、`^[a-z0-9][a-z0-9._-]{2,31}$`。

口令不作为值对象长期放在内存以外的地方。`PasswordHasher` 接口：

```go
type PasswordHasher interface {
    Hash(password string) (string, error)
    Verify(hash, password string) (bool, error)
}
```

生产实现是 argon2id：time 3、memory 64 MiB、threads 2、salt 16 字节、key 32 字节。参数写进哈希字符串（PHC 格式），校验时读字符串里的参数，便于以后调高成本。比较用恒定时间。这是**策略**，测试用确定性的假 hasher，避免每个用例花 64 MiB。

禁用账号立即无法通过 `Session.Valid(now)`。`Valid` 要求 status 由仓储在装载时连接查询：账号 `active`、`revoked_at` 为空、`now < expiresAt`。

### 会话与 CSRF

登录成功：

1. 生成 32 字节会话令牌和 32 字节 CSRF 秘密，都来自 `crypto/rand`。
2. 库里只存 SHA-256。
3. `Set-Cookie: nex_session=<base64url令牌>; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=43200`
4. `Set-Cookie: nex_csrf=<base64url秘密>; Secure; SameSite=Strict; Path=/; Max-Age=43200`
5. 会话 12 小时绝对过期，滑动续期只更新 `last_seen_at`，不延长 `expires_at`。

`nex_csrf` 不设 HttpOnly，前端才能放进 `X-CSRF-Token`。会话 Cookie 保持 HttpOnly。

改变数据的 `/api/admin/` 请求必须同时满足：

- 会话有效。
- 头 `X-CSRF-Token` 等于 `nex_csrf` Cookie。
- `sha256(令牌)` 等于该会话的 `csrf_secret_hash`。
- `Origin` 为空或等于 `NEX_PUBLIC_BASE_URL` 的源。浏览器同源的 GET 不查 CSRF。

这是绑定到服务端会话的**同步令牌**，不只是双提交 Cookie。攻击站点读不到 `nex_csrf`，也伪造不了哈希。

`development` 且基址是 `http://localhost` 时允许不带 Secure，便于本地。其他环境缺 Secure 配置就拒绝启动。

登出：写 `revoked_at`，两个 Cookie `Max-Age=0`。

### 登录限流

`LoginLimiter` 是进程内固定窗口：**策略**接口，生产用内存实现。同一用户名每 15 分钟最多 5 次失败，同一 IP 每 15 分钟最多 30 次。超过返回 429，`Retry-After` 秒数。成功登录清掉该用户名的计数。API 进程多副本时这是尽力限制，第一阶段按单副本验收。失败响应与用户不存在使用同一句「用户名或口令不正确」，同 401，避免探明账号是否存在。

### 幂等

管理写入由 `WriteExecutor` 唯一拥有事务；`IdempotencyStore`、业务回调和 P1/P7 的 Tx 方法都使用显式传入的同一 pgx.Tx：

```go
type WriteResult struct {
    Status int
    Body   json.RawMessage
}

// 内部只调用一次 ports.Tx.Within；fn 不得自行提交或写 HTTP。
func (e *WriteExecutor) Execute(ctx context.Context, request IdempotentRequest,
    fn func(context.Context, pgx.Tx) (WriteResult, error)) (WriteResult, error)
```

1. 计算 principal_key、scope（方法、路由模板和资源 ID）及请求摘要。JSON 对象键排序、去掉语法空白，但保留数组顺序和字符串内部内容，不能改变输入语义。
2. 外层 Within 使用 READ COMMITTED，执行 `SET LOCAL lock_timeout = '2s'`，然后 `INSERT ... ON CONFLICT DO NOTHING RETURNING id`。插入成功才把同一 tx 交给业务回调；业务与幂等结果在同一次提交中变成 completed。
3. 若有同键未提交请求，INSERT 等待其提交或回滚。前者提交后，INSERT 无返回行，用后续新语句读取并锁定已提交的 completed 记录；摘要相同就重放，摘要不同返回 409 idempotency_mismatch。前者回滚时，本次 INSERT 可取得执行权；因并发清理等原因仍未得到行则重新尝试受限的登记步骤，不能跳过登记执行业务。
4. 管理命名空间不会提交 processing 行，正常路径没有 idempotency_in_progress 分支。若读取到异常遗留 processing，记录一致性错误并拒绝执行，不将其解释为仍在运行的正常请求。
5. 等锁超过 2 秒或请求截止时间，则回滚当前事务并返回 503 request_busy（Retry-After: 1）；不能为此提交 processing 或缓存失败响应。SET LOCAL 在事务结束后恢复，P6 的持久化批次 processing 状态不进入此执行器。

管理范围的 completed 记录保留 24 小时，清理由 nexadm jobs cleanup-idempotency 执行，不在请求路径删除。清理必须排除推送 scope：推送批次按 P6 的父子记录保留与成组清理协议处理，不能按子记录自己的到期时间独立删除。

HTTP 适配器负责认证、CSRF 和解码，然后调用 WriteExecutor。业务只返回 WriteResult，不直接写状态码。error 一律使业务、审计与本次幂等记录回滚；WriteResult 与 nil error 才提交后返回。直接保存/发布的 edit_conflict 返回 error；建议审核发现基线过期或存在草稿时，返回 409 的 WriteResult，只持久化 conflict 决定、审计与重放结果。具体规则以总契约的事务决定表为准，不能根据同为 409 或 code 相同就猜测提交行为。GET 不使用幂等键。

CLI 和 worker 没有 HTTP 幂等执行器时，由各自最外层命令调用一次 Within，再调用同一批领域 Tx 方法。不得依赖 context 里的隐式嵌套事务。

长操作第一阶段不存在。发布是短事务，幂等记录保存已提交的最终响应。P6 的批量推送只复用幂等表和请求摘要规则，不复用管理 WriteExecutor；它的批次父记录可以跨事务保持 processing，条目记录则与该条业务结果同事务完成。

### 审计

```go
type AuditEvent struct {
    ActorType    string // admin, system, ingest
    ActorAdminID *AdminID
    Action       string
    TargetType   string
    TargetID     string
    Changes      json.RawMessage
    RequestID    string
    JobID        string
}
```

`Auditor.Record` 只 `INSERT`。应用数据库角色在迁移里被授予 `INSERT, SELECT`，没有 `UPDATE, DELETE`。迁移角色单独使用。这是**追加日志**，不是通用事件总线。不要做成事务提交后的异步钩子，否则发布成功但审计丢失。

`Changes` 允许的形态是 `{ "field": { "from": ..., "to": ... } }`。写入前 `Sanitize` 丢掉键名匹配 `password`、`token`、`secret`、`authorization` 的节点，正文超过 500 字的值改成 `"omitted"`。调用方传业务字段差异，不传整份原始 HTML。

没有管理接口可以改审计。管理端「操作历史」如果要做，只读 `GET /api/admin/audit?target_type=&target_id=`，本包提供查询。

## 初始化命令

`nexadm admin init` 读 `NEX_ADMIN_USERNAME`、`NEX_ADMIN_PASSWORD`。口令长度 12 至 72 字节（argon2 前的原始字节上限），拒绝 `password`、`admin`、`changeme`、`nexclub`。用户已存在时什么都不改。`--reset` 才更新哈希并撤销该用户全部会话。标准输出只有用户 ID。

## HTTP

| 方法与路径 | 行为 |
| --- | --- |
| `POST /api/admin/session` | 登录，不要求 CSRF（还没有会话） |
| `DELETE /api/admin/session` | 登出，要求 CSRF |
| `GET /api/admin/session` | 返回 `{ "admin_id", "username", "expires_at" }`，不返回令牌 |

登录体：`{ "username", "password" }`。密码字段不进访问日志。

中间件 `RequireAdmin` 解析 `nex_session`，失败 401。`RequireCSRF` 包住所有非 GET 的 `/api/admin/`。两者都由 P0 的 `Mount` 接收，P1 的路由注册在这条链之后。

## 测试

- 错误口令与不存在的用户，响应体一致。
- argon2 假实现下，正确口令得到会话；库中 `token_hash` 不等于 Cookie。
- 过期、撤销、禁用账号都是 401。
- 无 CSRF 头的发布返回 403；头和 Cookie 不一致返回 403；与库中哈希不一致返回 403。
- 同幂等键同体重放得到第一次的响应，业务函数只执行一次。
- 同键不同体 409，业务函数不执行第二次。
- 同键并发请求在前一个提交后重放 completed；前一个回滚时后一个取得执行权，不产生正常的 processing 响应分支。
- 首请求超过等锁上限时，第二个请求回滚并返回 request_busy；使用原键重试可得到已提交结果。
- 直接发布的版本冲突回滚，建议审核的已知冲突只提交决定与幂等响应，两者都不能留下半次资源发布。
- 审计角色执行 UPDATE 被数据库拒绝。
- 事务回滚后没有审计行，也没有幂等完成行。
- 写回调调用 P1 发布后再返回技术错误，发布投影、版本、审计、幂等结果和 River 任务同时回滚。
- 写回调返回需要持久化的冲突结果时，只提交冲突决定与幂等响应，不改正式内容。
- 提交失败时不向客户端发送成功状态；同一次写命令只开启一个事务。

## 完成定义

P1 的处理器可以只从 `context` 取 `AdminID`。一条发布集成测试同时断言审计行和幂等重放。P5 按这里的 Cookie 名和头实现登录，不再另定会话方案。
