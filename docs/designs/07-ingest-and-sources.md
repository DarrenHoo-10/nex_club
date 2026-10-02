# P6 采集与信源技术方案

本包把 GitHub、RSS 和外部推送收成同一种原始资料。它不发布资源，不调用大模型，不计算热度。指标采样只写 `external_metric_snapshots`。

归属：`internal/ingest`、`internal/sources/github`、`internal/sources/rss`、`internal/sources/push`、`db/queries/ingest.sql`。本包维护 sources、source_runs、ingest_credentials、raw_items、raw_item_revisions、raw_item_discoveries、external_metric_snapshots 的行为；建表迁移纳入 P0 第二阶段 schema 基线，本包不重复 CREATE TABLE。

独立合并与空库集成测试前，P0 第二阶段 schema 基线和 P1 IdentityLookup 契约必须存在；P7 的入队端口实现可先用假实现替代。

## 表结构增量

第二阶段基线为共用 idempotency_requests 增加 parent_id、item_index、request_meta，用于批次父记录和逐条结果；字段、外键及约束详见数据库文档。本包复用该表，不复用管理端 WriteExecutor，也不新增应用表。

## 功能点

- 登记信源：类型、角色、可信级、频率、检查点、可自动更新的字段、是否允许展示全文。
- 暂停信源后不再抓取，也不接受该信源的推送。已发布内容保持原样。
- GitHub 适配器：按稳定仓库 ID 识别改名，遵守限流和条件请求。
- RSS/Atom 适配器：增量检查点，缺正文时只存摘要并标记需要提取。
- `POST /api/ingest/v1/items`：按信源令牌鉴权，单次最多 50 条，逐条返回结果。
- 同一资料重复推送不建第二份；内容哈希不变不建新修订。
- 成功持久化之后才推进检查点。
- 仓库 Star、fork、issue 写入指标快照。未知不写 0。

不做：网页选择器、X、公众号、自动发布、用 Star 改变第一阶段热度。

## 领域对象

### 信源

```go
type Source struct {
    id            SourceID
    key           SourceKey
    kind          SourceKind // github, rss, json, web, x, wechat, external
    mode          Mode       // content, signal, internal
    trust         Trust      // official, verified, community, excluded
    enabled       bool
    config        SourceConfig
    checkpoint    Checkpoint
    editVersion   int64
    autoFields    []catalog.FieldPath
    allowFulltext bool
}
```

`SourceConfig` 是接口，按 kind 解码。JSON 配置不放凭据，凭据只存 `credential_ref` 环境变量名。接口响应不回显这个名字对应的值。

`excluded` 或 `enabled=false` 时，调度和推送都拒绝，返回 403 `source_disabled`。

角色：

| mode | 行为 |
| --- | --- |
| content | 资料进入后续加工队列 |
| signal | 只写指标或发现关系，不入加工 |
| internal | 只存原始资料，不入加工，也不进公开输出 |

### 资料身份

```go
type RawItem struct {
    id            RawItemID
    ownerSourceID SourceID
    identityKey   IdentityKey
    canonicalURL  string
    currentRev    *RawRevisionID
}

type RawRevision struct {
    number       int64
    contentHash  string
    title        string
    excerpt      string
    bodyText     string
    sourcePublishedAt *time.Time
    payload      json.RawMessage
}
```

身份键**策略** `IdentityStrategy`，按顺序使用第一个有值的结果：

1. `github:repository:<数字 ID>`
2. `url:<规范化永久原文 URL>`。优先使用通过来源校验的条目 link；GUID 明确声明永久链接且通过同样校验时也可提供此 URL。URL 校验包括确认为该条原文的稳定地址，不能仅凭字符串像网址就采用
3. 仅在缺少可信永久原文 URL 时，使用 `rss:source:<稳定sourceID>:guid:<经过无歧义编码的本地guid>`
4. 无法确定时 `source:<sourceKey>:<来源本地 ID>`，并在发现记录上标 `needs_review`

URL 规范化与 P1 工具身份使用同一个 `catalog/urlcanon` 函数。只删除已知跟踪参数：`utm_*`、`fbclid`、`gclid`、`ref`、`mc_cid`、`mc_eid`。不升级 http 到 https，不删除其他查询参数。

内容哈希 `hash.v1`：对标题、摘要、正文做 NFKC、压缩空白、去掉首尾空白，不改变大小写，然后 SHA-256。抓取时间不进哈希。哈希相同则只更新 `last_seen_at` 和发现计数。

主来源 `owner_source_id` 拥有修订权。另一信源再次看到同一身份键，只增加 `raw_item_discoveries`，不改正文，不替换主来源。

同时有本地 GUID 和可信 link 时，资料 identity_key 使用 url，GUID 保留在发现记录的 source_item_key，不抢占跨源身份。两个来源的 GUID 相同但可信原文链接不同，或都没有可信 URL 时，形成不同资料；两个来源的可信原文链接相同则共享资料并各保留发现关系。不能删除信源命名空间后直接比较本地 GUID；无法确认时分别保留，再审核关联。

### 适配器

```go
type Adapter interface {
    Kind() SourceKind
    Fetch(ctx context.Context, src Source) (FetchBatch, error)
}

type FetchBatch struct {
    Items      []IncomingItem
    Checkpoint Checkpoint
    Metrics    []MetricSample
}
```

`IncomingItem` 是防腐对象：来源 ID、URL、标题、摘要、正文、原文发布时间、抓取时间、原始 JSON、是否历史回灌。适配器负责把 GitHub 或 RSS 填进这个结构。`IngestService` 不解析 GitHub 字段名。

`Fetch` 返回错误时，调用方不写 `checkpoint_after`。`FetchBatch.Checkpoint` 只有整批 `Ingest` 提交成功才保存。

## 写入服务

`IngestService.AcceptTx(ctx, tx, source, item)` 只使用传入的单条事务；定时采集的外层 Accept 命令为每条调用一次 Within，推送由下文的批次协调器安排单条事务，不嵌套开启事务：

1. 锁定信源行，核对 `edit_version` 与任务启动时一致。不一致则这条结果记 `stale_source`，不推进检查点。
2. 算身份键和内容哈希。
3. 插入或命中 `raw_items`。身份键已存在且主来源不同：只写发现关系。
4. 哈希变化才插入 `raw_item_revisions` 并移动 `current_revision_id`。
5. 若 `mode=content` 且有新修订，同一事务入队 P7 的 `editorial.extract` 或直接 `editorial.prefilter`（已有正文则跳过提取）。P7 未合并时入队函数是端口 `PipelineStarter`，fake 记录调用即可，本包测试不启动模型。
6. 写 `source_runs` 的计数。

批量推送逐条调用 AcceptTx，每条业务与其处理结果独立提交；字段不合法等确定性拒绝不会影响其他条。基础设施故障中断本次请求，已经完成的条目保持提交，下次按批次结果记录续作。响应：

```json
{
  "results": [
    { "source_item_key": "abc", "status": "created", "raw_item_id": "..." },
    { "source_item_key": "abc", "status": "unchanged", "raw_item_id": "..." },
    { "source_item_key": "bad", "status": "rejected", "code": "invalid_argument" }
  ]
}
```

`status`：`created`、`revised`、`unchanged`、`discovered`、`rejected`。

检查点在整次定时抓取的所有 `Accept` 成功后，另一次事务里写入。中间有失败则检查点停在原处，已写入的资料保留，下次抓取靠身份键幂等。

原文发布时间缺失就保持 null。禁止用 `fetched_at` 填充 `source_published_at`。

## GitHub 适配器

配置：`owner`、`name`，或搜索查询。首批实现「已知仓库列表」，搜索留在配置结构里但 `Fetch` 遇到 `mode=search` 返回明确错误 `not_implemented`。

流程：

1. `GET /repos/{owner}/{name}`，带上次检查点里的 `etag`。304 表示元数据未变，仍可按策略跳过指标。
2. 使用响应里的数字 `id`，不用 `full_name` 当身份。发现 `full_name` 变化只更新后续加工的候选字段，身份键不变。
3. README 用 contents API 取默认分支的 `README`，作为正文；失败则正文为空，不把描述当成正文。
4. Star、fork、open_issues 成为 `MetricSample`，`observed_at` 用响应头 `Date`，没有则用抓取时间。
5. 遇到 403 或 429 且带 `Retry-After` 或 `x-ratelimit-reset`：任务返回可重试错误，River 按该时间延迟。认证失败 401 标 `failed` 且不自动重试。

条件请求的 ETag 放在 `sources.checkpoint`：`{ "etag": "...", "repository_id": "123" }`。

归档标志 `archived` 只作为资料字段变化，不调用隐藏接口。

## RSS 适配器

使用编码后的 feed URL。检查点 `{ "last_build": "<rfc3339>", "seen_guids": ["...最多200条"] }` 中的 GUID 仅用于该来源增量观察，不决定跨源资料身份。条目仍按“平台全局 ID、可信原文 URL、来源内 GUID”的优先级解析身份；GUID 与来源内观察记录共同确认同一条目且哈希不变时跳过。没有 GUID 时，source_item_key 使用规范化永久链接。

`published` 解析 RFC 3339 或 RFC 1123，失败则该条 `source_published_at` 为空并在 `stats` 记 `bad_date`，条目仍入库。

feed 超过 2 MB 或单条正文超过 200 KB 时截断并标 `truncated=true` 在原始载荷里。不跟随到私网地址。HTTP 客户端与下面的安全策略共用。

## 外部推送

`ingest_credentials.token_hash` 存 SHA-256。令牌只在 `nexadm ingest token create --source <key>` 的标准输出显示一次。

认证：`Authorization: Bearer <token>`。服务端查出凭据，核对未过期、未撤销、`scopes` 含 `items:write`、信源仍启用。请求体里的 `source_id` 若与令牌信源不符，整批 403。不信任调用方自报信源。

### 推送批次与逐条结果

推送只复用幂等表和规范化摘要函数，不调用管理 WriteExecutor。幂等在声明的保留期内生效，按下面三个层次持久化：

1. **登记整批请求。** 在短事务中按 principal_key=`ingest:<credential_id>`、scope=`ingest.batch.v1` 和客户端 Idempotency-Key 创建父记录，保存整批 request_hash，以及 request_meta 中的有序 item_hashes、item_count、source_id 和 source_edit_version，状态 processing，然后提交。重复键用新语句读取既有父记录；摘要不同在任何 AcceptTx 前返回 409 idempotency_mismatch；已 completed 则重放保存的完整响应。父记录的 processing 在这里可以持久存在，这是与管理幂等协议的明确区别。
2. **每条业务与每条结果一起提交。** 每条开始自己的 Within，先以 FOR SHARE 锁定父记录并核对摘要及状态；使用 parent_id 与原始数组 item_index 定位子记录。子记录的 scope=`ingest.item.v1:<parentID>`、idempotency_key 为 item_index 字符串，request_hash 为对应条目摘要。已完成则直接使用它的原始结果，不再调用 AcceptTx；否则插入子记录，调用 AcceptTx，并将 created/revised/unchanged 等结果和子记录 completed 状态同事务提交。业务产生修订和后继入队的事务就是这个单条事务。
3. **汇总完整响应。** 全部条目有终态结果后，在短事务中 FOR UPDATE 锁定父记录，按 item_index 读取全部已完成子结果并校验数目，再保存整批响应、置 completed 后返回。并发汇总者重放同一结果。父行锁与子事务的共享锁协调，避免清理或汇总与正在提交的条目交叉。

子记录仍使用数据库唯一约束挡住并发重复。确定性单条拒绝可在保存点回滚该条业务写入后，将 rejected 结果与子记录提交；SQL 基础设施错误则回滚整个单条事务，父记录维持 processing，返回可重试的 503，不把暂时失败缓存成最终拒绝。全批格式、大小、身份和权限在登记前校验，不能通过另一份请求替换已登记内容。

崩溃恢复时客户端重发相同键和请求体，协调器跳过所有已完成子项，只处理缺失项。即使其他批次已经把同一资料从 v1 更新为 v2，旧批次的已完成 v1 项也不会再执行，因此不会回退正文，且原来的 created 结果仍能重放。仅凭 raw_items 身份键去重无法提供这个保证。

并发重试可以共同遍历同一批次，但必须通过同一子记录登记事务竞争单条执行权，不能绕过它。不能长时间持有覆盖 50 条处理的管理事务，也不能最后才登记整批键。接管父记录不需要新的业务轮次；显式新推送使用新幂等键。

完成后的父记录及子记录作为一组至少保留 24 小时，期限从整批完成计算。只允许在父记录 completed 且到期后，锁定父行、先删子记录再删父记录；未完成父记录及其已完成子项不得分别按创建时间清理。超过预期时长的未完成批次告警并人工处理。保留期外不承诺重放旧结果，客户端不能把过期重试当作自动恢复。

单次最多 50 条，正文合计最大 1 MB。超出 400。

## HTTP 安全

抓取客户端 `internal/sources/httpx`：

- 只允许 `http` 和 `https`。
- 每次连接前解析 DNS，拒绝环回、私网、链路本地、云元数据地址 `169.254.169.254`，以及指向它们的重定向。
- 最多 3 次重定向，响应体上限 2 MB，超时 10 秒。
- 不把原始载荷里的 `Authorization` 写进 `raw_payload`。

这是适配器共用的**防腐层**，各源不自己 `http.Get`。

## 调度

与 P3 相同的数据库扫描：`enabled` 且 `next_fetch_at <= now` 的信源，事务内锁定并插入 `source_runs`，`run_key = sourceID + scheduled_for + rerun`。唯一键挡住并发双开。River 任务 `ingest.fetch`。

手动「立刻抓一次」把 `rerun` 设为管理员提供的整数，不复用旧 `run_key`。

worker 在抓取事务提交前崩溃：检查点未前进，任务可重试，`Accept` 幂等。

## 和资源身份的边界

本包发现 GitHub 数字 ID 后调用 `IdentityLookup`。命中则在内存结果里记下 `resource_id`，交给 P7 做差异，不在这里更新资源。未命中且 `mode=content` 时也不创建 `resources` 行。新建资源只发生在管理员采纳建议时，由 P1 执行。

信号模式的指标如果还没有资源，快照先不写，`source_runs.stats.metrics_unmatched` 加 1。不建一个空资源来挂 Star。

## 测试

- 同一推送并发两次，只有一个 `raw_items`。
- 两个 RSS 源使用相同 guid=123 但可信原文链接不同，建立两份资料；没有可信 URL 时同样依靠 source_id 隔离。
- 两个来源各有非永久 GUID，同时 link 指向同一可信永久原文 URL，只建立一份资料，保留两个 source_item_key 发现关系。
- 哈希不变不增加修订；正文变化修订号加 1。
- 第二信源看到同一 URL，发现行增加，主来源正文不变。
- `Accept` 返回错误时检查点 JSON 与抓取前相同。
- GitHub 夹具：改名响应仍得到原 `github:repository:` 键。
- 私网 URL 和元数据地址被客户端拒绝。
- 跟踪参数不同、其余相同的 URL 命中同一资料。
- 暂停信源后推送 403，已有修订还在。
- 令牌不能把 `source_id` 写成另一个信源。
- 批次同键不同请求在首条 AcceptTx 前拒绝；并发同键同体每条只执行一次。
- 第 20 条完成后进程退出，恢复时跳过前 20 条，并重放其原始 created/revised 结果。
- 单条提交后、批次完成前被另一个批次更新为 v2，旧批次重试不再执行该条，资料保持 v2。
- 所有条目完成但汇总响应尚未提交时退出，恢复只汇总结果，不再写资料或入队。
- 未完成父批次中的已完成子项不被 24 小时清理任务删除；单条技术错误回滚不删除此前条目的结果。

夹具放在 `testdata/`，测试不访问 `api.github.com`。

## 完成定义

两条真实来源可以先用录制的 HTTP 夹具代替。重复运行导入，资源表行数不因本包增加。检查点在失败时停住，在成功时前进。
