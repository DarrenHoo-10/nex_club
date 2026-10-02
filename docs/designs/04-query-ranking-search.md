# P3 查询、搜索与排名技术方案

本包是公开读路径和行为统计。它读取 P1 写下的投影，不修改投影，不决定谁可以登录。

归属：`internal/search`、`internal/ranking`、`internal/metrics`、`db/queries/search.sql`、`db/queries/ranking.sql`。实现 `ports.RankingEnqueuer`。

## 功能点

- `GET /api/v1/resources` 组合 kind、关键词、多标签交集、四种排序和游标。
- 按 ID、按 slug 取已发布详情。未发布、演示（生产配置下）和不存在都是 404。
- 标签计数、当前推荐位。
- 接收详情阅读和外链点击，时间窗去重，并维护日汇总。
- 每 5 分钟为每个板块生成热度与推荐快照；失败继续用上一批。
- 没有成功批次时，推荐排序的 `effective_sort` 为 `latest`。

不做：浏览器里计算热度、把 GitHub Star 算进第一阶段热度、个性化推荐、独立搜索引擎。

## 读模型

查询服务不装载 `catalog.Resource` 聚合。它有自己的只读结构 `PublicResource`，字段等于 [总契约](00-split-and-contracts.md) 的卡片和详情。仓储 SQL 必须连接 `resources`，条件固定为：

```sql
r.status = 'published'
and (sqlc.arg(include_demo)::bool or not r.is_demo)
```

只查 `resource_publications` 不算公开。排名条目同样在查询时过滤，下架立即生效，不必等下一批快照。

`card` 用 P1 放在 `internal/catalog/present` 的纯函数生成，只依赖 kind 和 details。P3 不另写中文标签。

## 列表查询对象

```go
type ListQuery struct {
    Kind   catalog.Kind
    Text   string
    TagIDs []catalog.TagID // 由 slug 解析，关系是交集
    Sort   Sort            // 空表示按默认规则
    Cursor string
    Limit  int             // 默认 24，最大 60，超出 400
}
```

默认规则：**有非空 q 且没传 sort → `relevance`；没传 q → `recommended`。** 用户传了 sort 就在匹配集合里使用该排序。

标签参数重复表示交集：`tag=chat&tag=writing`。未知 slug 使结果为空，不是 400。一个请求最多 8 个标签。

`q` 去掉首尾空白后最长 80 个字符。空字符串视为无查询。

### 相关度

匹配层级，数字小的更靠前：

| 级 | 条件 |
| --- | --- |
| 1 | 标准化标题等于标准化 q |
| 2 | 标题或别名包含 q |
| 3 | 标准标签名或标签别名包含 q |
| 4 | 简介包含 q |
| 5 | 教程正文或步骤包含 q |

同级再按第一页选定排名批次的推荐分降序、id 降序；后续页固定使用该批次。第一页没有批次时，将 RankingRunID 固定为 null，整次分页推荐分都为 0，即使途中生成新批次也不切换。该批次缺少的新资源按推荐分 0 处理。用户指定 heat 或 latest 时不再按层级排序，只保留“至少命中一级”的集合。

标准化与 P1 的 `search_text` 同一函数。包含判断：

- q 的码点长度大于 2：优先 `search_text ILIKE '%' || q || '%'`，并依赖 `pg_trgm` GIN。
- 码点长度小于等于 2：同样的 ILIKE，在显式只读事务中执行 `SET LOCAL statement_timeout = '150ms'`，后续查询必须使用该事务的同一连接。成功后提交；超时、取消或其他错误先回滚再归还连接，返回 503 unavailable 而不返回半截结果。禁止直接在池连接上执行会话级 SET，也不能在事务外执行 SET LOCAL。150ms 是单语句限制，另有请求级 deadline 限制总耗时。

第一阶段不做分词器。回归样例固定为：配音、本地、写代码、Claude、llama.cpp。它们写进 `search/testdata/queries.yaml`，对种子数据断言期望的 slug 顺序。

### 最新

排序键：`first_published_at DESC, id DESC`。没有发布时间的行不会进入公开查询，因为只有发布事务才把状态改成 `published`。

### 游标

```go
type cursorV1 struct {
    V            int       `json:"v"` // 1
    KeyID        string    `json:"kid"`
    Kind         string    `json:"kind"`
    Sort         string    `json:"sort"`
    FilterHash   string    `json:"fh"`
    RankingRunID *string   `json:"run,omitempty"`
    Position     *int      `json:"pos,omitempty"`
    PublishedAt  *string   `json:"at,omitempty"`
    MatchTier    *int      `json:"tier,omitempty"`
    RecommendationScore *string `json:"score,omitempty"` // numeric(7,4) 的精确十进制串
    ID           string    `json:"id"`
    IssuedAt     string    `json:"issued_at"`
    ExpiresAt    string    `json:"expires_at"`
}
```

编码为 `base64url(payloadBytes).base64url(mac)`，mac 为 `HMAC-SHA256(key, "cursor.v1\x00" || payloadBytes)`。payload 包含 KeyID，使用 P0 独立配置的 NEX_CURSOR_SECRET 签名；不复用访客密钥。验签根据有长度限制的 KeyID 查允许的当前/旧密钥集合，使用原始 payloadBytes 计算 MAC，再用 hmac.Equal 比较，不能重新编码 JSON 后比较。未知 key ID 或验签失败返回 400 cursor_stale，不向客户端回显密钥。

FilterHash 是 kind、标准化 q、有序标签、请求排序、搜索规则版本 search.v1 的 SHA-256 前 16 字节；Sort 保存首次实际采用的 effective_sort。验签后再校验条件摘要、必需字段、期限以及批次 kind 和 ready/retired 状态。旧验签密钥按 P0 的轮换宽限保留，KeyID 及 issued_at/expires_at 均被签名覆盖。

签发时间与截止时间在后续游标中沿用，不随翻页延长；最多 24 小时，并不得晚于绑定批次的 expires_at。第一页从推荐降级到 latest 后，下一页继续使用游标里的 latest，不因新批次出现而中途改变排序。

后续游标使用当前活动 KeyID 和对应密钥签名，继续沿用最初 issued_at、expires_at 与排名批次；旧密钥只用于验签，不因使用旧游标而重新以旧密钥签名。

热度和推荐的游标绑定 `ranking_run_id`，下一页条件是 `position > cursor.position`。同一批次里位置唯一，所以不会因分数刷新而重复或漏项。批次 `expires_at` 已过返回 400 `cursor_stale`。快照保留 24 小时，由 `expires_at = computed_at + 24h` 表达。

最新排序的游标带最后一条的 `first_published_at` 和 `id`，条件是 `(first_published_at, id) < ($at, $id)`。

相关度游标必须带 MatchTier、RecommendationScore 和 ID，以及固定的 RankingRunID（允许 null）。排序键为 `match_tier ASC, recommendation_score DESC, id DESC`。下一页条件为：

```sql
match_tier > $tier
OR (match_tier = $tier AND recommendation_score < $score)
OR (match_tier = $tier AND recommendation_score = $score AND id < $id)
```

比较用与排名表相同精度的 numeric，不能在 Go 中转成浮点再写游标。不需要反查上一页最后一项，即使它已下架也能继续。绑定批次过期返回 cursor_stale；资料文本或标签在翻页期间变化时，按实施方案重新搜索，不承诺跨内容修改的结果快照一致。没有 q 却显式要求 relevance 时先归一为 recommended，再按有无批次决定 effective_sort。

`total`：无 q、无标签、排序为最新时可以 `count(*)`。其他组合第一阶段返回 `null`。前端看到 `null` 不显示数字。

## 行为与热度

```go
type Event struct {
    id         uuid.UUID
    resourceID catalog.ResourceID
    typ        EventType // detail_view, outbound_click
}
```

`POST /api/v1/events`：

1. 访客 Cookie `nex_vid`，32 字节随机，`SameSite=Lax`，`Path=/`，180 天。没有就签发。
2. `visitor_hash = hex(hmacSHA256(NEX_SESSION_SECRET, vid))`。不接受客户端传来的访客 ID。
3. 资源必须当前公开，否则该条计 `duplicate` 以外的忽略，不报 404，避免用事件接口探测草稿。整批仍 202。
4. `bucket_start` 是服务端 `Clock` 向下取整的 5 分钟。
5. 插入事件。主键冲突或 `(resource, visitor, type, bucket)` 冲突都算重复，不增加日汇总。
6. 真正插入时，同一事务把 `resource_metrics_daily` 对应 UTC 日期的列加 1。

单请求最多 20 条。同一 `nex_vid` 每分钟最多接受 60 条新事件，超出 429。内存限流，单副本。

日汇总的重建命令 `nexadm metrics rebuild --date YYYY-MM-DD` 按事件表重算该日并替换，不在原值上再加一遍。

### 热度规则 `heat.v1`

**策略**接口：

```go
type HeatRule interface {
    Version() string
    Score(events []EventBucket, now time.Time) float64
}
```

`EventBucket` 是 SQL 聚合结果：资源、类型、`bucket_start`、次数。衰减用桶起点，最大偏差 5 分钟。

- 窗口：`now-7d` 至 `now`。
- 权重：阅读 1，外链 3。
- 单桶贡献：`weight * count * 0.5 ^ (ageDays / 3)`，`ageDays` 用秒除以 86400。
- 原始分：`log1p(贡献和)`，自然对数。
- 展示分：在同一 kind 的公开非演示资源里，把原始分线性映射到 0–100。全 0 则全 0。最大值等于最小值且大于 0 时，这些资源都是 100，其余是 0。
- 没有事件的资源热度是 0。

热度平分打破：`heat_score DESC, first_published_at DESC, id DESC`。

公式的参考实现放在 Go，测试用固定时钟和手算样例。SQL 只负责把桶聚合出来，避免数据库浮点和 Go 浮点两套公式。

### 推荐规则 `rank.v1`

分量都是 0 至 100：

```text
recommendation = 0.60 * quality + 0.25 * heat + 0.15 * freshness
```

`quality` 来自投影，缺省按 0。`freshness`：`freshness_eligible` 为 false 时是 0；否则 `100 * 0.5 ^ (ageDays / 14)`，年龄从 `first_published_at` 算。超过很久就接近 0，不设硬截断。

多样性是另一个**策略** `DiversityPolicy`：

- 只调整推荐序的前 12 个位置。
- 主分类相同的资源不要连续超过 2 个。
- 贪心：下一个候选若会形成第 3 个连续同类，就向后找第一个不同主分类的候选交换上来；找不到就保持原候选。
- 主分类为空的资源视为各自独立的类，不互相算连续。
- 第 13 位之后按推荐分排序，不再打散。

平分打破：`recommendation_score DESC, first_published_at DESC, id DESC`。位置从 1 连续编号，包括 12 位之后的全部公开资源。

`reason_code`：质量分 ≥ 70 且热度 < 30 写 `editor_pick`；热度 ≥ 70 写 `recent_attention`；新鲜度 ≥ 70 写 `new_entry`；否则空。只写一个，按这个顺序命中。不生成自然语言赞美。

Star 增量不进入 `heat.v1`。P6 以后新增 `heat.v2`，用新的 `rule_version`，不改旧批次的参数 JSON。

## 排名快照

```go
type Run struct {
    id          uuid.UUID
    kind        catalog.Kind
    ruleVersion string
    status      RunStatus // building, ready, failed, retired
    isCurrent   bool
}
```

`RankingService.Build(kind)`：

1. 插入 `building` 行，`parameters` 保存权重、半衰期、窗口和多样性参数的副本。
2. 读出该 kind 全部公开资源的质量分、发布时间、主分类、`freshness_eligible`，以及 7 日内事件桶。
3. 在内存算分、排序、打散，批量插入 `ranking_entries`。
4. 校验位置 1..n 都存在且资源 kind 匹配。失败则把该行标 `failed` 并返回，不改 `is_current`。
5. 切换事务：`pg_advisory_xact_lock(hashtext('ranking:' || kind))`，旧的 current 改 `retired` 且 `is_current=false`，新行 `ready`、`is_current=true`、写 `computed_at` 和 `expires_at`。
6. 锁内提交。中途崩溃就留下 `building`，下一轮不把它当成当前批次。

部分唯一索引保证一个 kind 只有一个 `is_current`。

`RankingEnqueuer.Enqueue` 插入 River 任务，参数 `{ "kind": "tool" }`。`river` 唯一键是 `ranking.refresh/<kind>`，已有未完成任务时不再插第二行。发布事务只入队，不等计算。

调度：worker 每分钟扫描。某 kind 没有 `ready` 批次，或最新 `computed_at` 早于 4 分钟，就入队。周期写在数据库扫描里，不依赖 River 进程内定时器刚好触发。启动时同样扫描一次。

任务失败：批次 `failed`，日志 error，公开接口继续读旧的 current。一个 kind 超过 15 分钟没有 `ready` 且 `computed_at` 新于 15 分钟内的批次，worker 打 `ranking_stale` 告警日志。没有过任何成功批次时列表降级为最新。

清理：`nexadm ranking prune` 删除 `retired` 且 `expires_at < now()` 的运行，条目级联删除。不删除 `current`。

## 推荐位读取

`listFeatured`：`enabled`，`starts_at <= now`，`ends_at` 为空或大于 now，资源仍公开。按 `position` 升序。资源已下架的槽不返回，也不把后面的位置重新编号；前端按返回顺序展示。

## 设计模式落点

| 模式 | 落点 |
| --- | --- |
| 查询对象 | `ListQuery` |
| 策略 | `HeatRule`、`DiversityPolicy`、排序键的 `SortPlan` |
| 快照 | `ranking_runs` + `ranking_entries`，分页读快照不读实时分 |
| 装饰 | 事件写入同时更新日汇总，汇总逻辑不散落在处理器 |
| 事务性发件箱 | 发布事务里的 `Enqueue`，计算在 worker |
| 空对象式降级 | 无批次时 `SortPlan` 换成最新，而不是处理器里特判 |

`SortPlan` 把「用哪一列、游标怎么解」收进一个类型，HTTP 处理器只调用 `Search.List`。

## 测试

单元：衰减手算、对数与归一化、多样性的 3 连分类、平分时 ID 次序、游标篡改和过期、短词与长词走同一匹配函数。

PostgreSQL：

- 草稿 slug 在列表、详情、by-slug、标签计数、推荐位中都不可见。
- 发布后下一次查询可见；隐藏后下一次查询 404，旧排名里的该 ID 也被滤掉。
- 翻页过程中插入新 `ready` 批次，旧游标仍沿旧批次走完。
- relevance 同层级跨多个分数及相同分数跨多个 ID 的分页，无重复或遗漏。
- relevance 第一页无排名批次时，第二页即使生成批次仍按 0 分继续；第一页有批次时锁定该批次。
- 上一页最后一项下架后仍可继续；签名篡改、过滤条件变化、批次过期得到 cursor_stale。
- 游标用独立密钥及恒定时间验签；轮换访客密钥不影响翻页，旧游标可用保留的旧 key ID 验签，移除后明确失效。
- 用同一个池连接先执行短查询超时或取消，再执行普通查询，确认 statement_timeout 没有泄漏到后一个请求。
- 同一事件 ID 重放，日汇总只加 1；同一窗口更换事件 ID 也只加 1。
- `is_demo=true` 且 `include_demo=false` 时公开接口不可见。
- 排名任务在插入条目后、切换前失败，旧 current 不变。
- 种子回归查询五条关键词。

P3 的集成测试自己插入投影行，不调用 P1 的 HTTP，这样两条线可以并行。另有一个小的端到端测试，在 P1 合并后加到 `server/internal/integration`，断言发布会入队。

## 完成定义

三个 kind 都能按契约返回卡片。重复刷新事件不会使热度线性增长。worker 停掉时列表仍返回上一批推荐，`ranking_computed_at` 是那一批的时间。
