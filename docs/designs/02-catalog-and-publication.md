# P1 目录与发布技术方案

本包实现资源从草稿到公开投影的全部写入规则，以及标签、推荐位配置和种子导入。公开列表 SQL、热度公式和管理员登录不在这里。

归属目录：`internal/catalog`、`internal/publication`、`db/queries/catalog.sql`。实现 `ports.Publisher`、`ports.IdentityLookup` 和 `ports.Auditor` 的调用方。不实现 `Auditor` 本身，那个在 P2；P1 的测试用记录调用的 fake。

## 功能点

- 新建三种资源草稿，保存新修订，不改动已经公开的投影。
- 发布指定草稿：写投影、替换正式标签、首次发布时间、审计、事务内安排排名刷新。
- 隐藏、归档、按现有投影恢复公开。
- 标签、别名、同维度合并。
- 推荐位的创建与时间段冲突。
- `nexadm import` 重复执行现有 `src/data/*.json` 不产生第二份资源。
- 按身份键查找资源，供第二阶段采集复用。

不做：搜索排序、行为事件、会话校验（由 HTTP 层在进入服务前完成）、自动采集。

## 表结构增量

在 P0 的第一阶段 schema 基线中为 `resources` 增加（已同步数据库文档，P1 不重复建列）：

```sql
freshness_eligible boolean not null default true
```

历史种子置 `false`，真实新发布保持 `true`。排名用它决定是否给予新鲜度，见 P3。不新增其他列。

教程原型里的 `notes` 放进修订 `details.notes`，不新建列。

## 领域对象

包 `internal/catalog` 放聚合和值对象。数据库行结构只出现在 `internal/store`。

### 值对象

构造函数失败就返回 `apperr`，聚合方法只接受已经合法的值。

| 类型 | 不变量 |
| --- | --- |
| `Kind` | `tool` `tutorial` `repo`，创建后不变 |
| `Slug` | `^[a-z0-9]+(?:-[a-z0-9]+)*$`，长度 1 至 80 |
| `Status` | 见状态机 |
| `QualityScore` | 整数 0 至 100 |
| `FieldPath` | 只能是下面白名单里的路径 |
| `IdentityKey` | `url:` 或 `github:repository:` 前缀，长度 1 至 300 |
| `ExternalURL` | `http` 或 `https`，有主机名，没有用户信息，拒绝 `localhost`、环回、链路本地和私网字面地址 |
| `EditVersion` | 大于 0 的 int64 |

`FieldPath` 白名单：`title`、`aliases`、`summary`、`body_markdown`、`cover_urls`、`primary_category_id`、`tag_ids`、`quality_score`、`recommendation_reason`，以及 `details.` 加各 kind 专属键。`slug`、`status`、`identity_key` 不能放进字段锁。

收费 `Pricing`：`unknown` `free` `paid` `freemium`。难度 `Level`：`unknown` `beginner` `advanced`。平台与部署方式首版枚举：

- 平台：`web` `desktop` `mobile` `api` `plugin`
- 部署：`hosted` `self_host` `local`

种子数据没有这两个数组时写空数组，不猜测。

### 专属字段策略

`Details` 是接口，三种实现。这是**策略**，不是在发布服务里写长的 `switch` 校验。

```go
type Details interface {
    Kind() Kind
    Validate() error
    CanonicalIdentity() (IdentityKey, bool)
}

type ToolDetails struct {
    WebsiteURL ExternalURL
    Pricing    Pricing
    Platforms  []Platform
    Deployment []Deployment
}

type TutorialDetails struct {
    Level     Level
    Minutes   int  // >= 0
    Steps     []Step
    Author    string
    SourceURL *ExternalURL
    Notes     string
}

type RepoDetails struct {
    GitHubRepositoryID string // 十进制，可空直到第二阶段确认
    FullName           string // owner/name
    Language           string
    License            string
    Archived           *bool
    LastActivityAt     *time.Time
}
```

`TutorialDetails.Validate`：`body_markdown` 与 `Steps` 至少有一个非空。这一条需要正文，所以校验函数是 `ValidateContent(body string, details Details)`，放在目录服务而不是只放在 Details 里。

`RepoDetails` 在状态变为 `published` 时允许还没有 `GitHubRepositoryID`。种子阶段仓库 ID 未知。一旦 ID 写上，就同步把 `identity_key` 设为 `github:repository:<id>`。已有不同的身份键时拒绝，不覆盖。

`ToolDetails.CanonicalIdentity` 返回 `url:` 加规范化官网。规范化规则与 P6 相同：小写主机、去掉默认端口和片段、去掉 `utm_*`、`fbclid`、`gclid`、`ref`，其余查询参数按键排序保留。

JSON 持久化时带 `schema_version: 1`。读取遇到更高版本返回 `invalid_argument`，不尝试猜测。

### 资源聚合

```go
type Resource struct {
    id                 ResourceID
    kind               Kind
    slug               Slug
    status             Status
    identityKey        *IdentityKey
    editVersion        EditVersion
    fieldLocks         FieldLockSet
    freshnessEligible  bool
    isDemo             bool
    draftRevisionID    *RevisionID
    firstPublishedAt   *time.Time
}

type Revision struct {
    id        RevisionID
    number    int64
    payload   Payload
    origin    Origin // manual, import, pipeline
    reason    string
    createdBy *AdminID
}
```

`Payload` 是一次修订的完整快照：标题、别名、简介、正文、封面、主分类、标签 ID、质量分、推荐理由、Details。热度和点击不进快照。

聚合上的行为：

| 方法 | 效果 |
| --- | --- |
| `NewDraft` | 状态 `draft`，版本 1，锁为空 |
| `SaveDraft(expected EditVersion, payload, actor, reason)` | 版本必须相等；生成下一修订号；人工来源把本次改动的路径并入字段锁；不碰发布投影 |
| `MarkPublished(at)` | 仅首次写 `firstPublishedAt`；状态改为 `published`；清空草稿指针；版本加 1 |
| `SetVisibility(next, expected)` | 按状态机；版本加 1 |
| `Unlock(paths)` | 只有命令显式带 `unlock_fields` 才移除锁 |
| `ApplyProposal(expected, payload, lockedSkip)` | 被锁字段保持旧值；版本检查失败返回 `edit_conflict` |

人工保存时，变更路径自动加入 `field_locks`。导入来源 `import` 也锁定种子里出现的内容字段，避免以后的流水线覆盖已经写好的简介。`pipeline` 来源不自动加锁。

### 状态机

```text
draft -> published | archived
published -> hidden | archived
hidden -> published | archived
archived -> published
```

`published -> draft` 不存在。恢复公开要求已经有发布投影，否则 `edit_conflict`，`code` 同为 `edit_conflict`，消息说明没有可恢复的版本。已发布资源再保存草稿时状态保持 `published`。

判断未发布草稿统一调用 catalog.HasUnpublishedDraft，不存独立布尔列，也不由管理查询或流水线各写一套判定：

```go
func HasUnpublishedDraft(draftID, publishedRevisionID *RevisionID) bool {
    return draftID != nil && (publishedRevisionID == nil || *draftID != *publishedRevisionID)
}
```

管理查询和流水线都加载同一资源的两个指针再调用该函数；采纳时在资源锁内重读。无指针为 false；有指针无投影为 true；指针指向不同修订为 true；指针等于已发布修订为 false。正常发布仍清空草稿指针，不因此放宽发布事务。

### 标签聚合

```go
type Tag struct {
    id        TagID
    dimension Dimension
    name      string
    slug      Slug
    status    TagStatus // active, disabled, merged
    mergedInto *TagID
}
```

`Merge(target)` 由领域服务 `TagMerger.MergeTx` 在调用方事务中执行：

1. 先取得 taxonomy 的事务级独占 advisory lock，再按 ID 锁定标签及受影响资源。要求源和目标均 active、同维度、非自身且不会形成合并环。
2. 将源标签的标准名行改为 `is_primary=false`，保留目标的标准名。再迁移源别名；如果存在需要消解的规范化冲突，保留目标记录并写审计，不能让目标出现两个标准名。
3. 对拥有源标签的资源插入目标关系，使用 `ON CONFLICT (resource_id,tag_id) DO NOTHING`，随后删除源关系，避免同时打过两个标签的资源违反复合主键。
4. 将正式投影中的 `primary_category_id=源ID` 改为目标 ID，重建受影响的 `search_text`，更新内容时间及资源 `edit_version`。未发布草稿和历史修订保持不可变。
5. 源标签置为 merged 并指向目标，写审计；按受影响板块在同一事务入队排名刷新。任一步失败整体回滚。

旧修订可能仍含合并前的标签 ID。预览与投影重建先解析 `merged_into_id` 到最终 active 标签，disabled 或无有效目标则拒绝；新保存的修订写入解析后的 ID。全局标签合并是管理员对字典的显式变更，不通过修改历史快照实现。

发布、预览所需的标签解析和草稿保存使用 taxonomy 共享事务锁；可能发布的外层流程必须先取得共享锁，再锁资源。标签合并先取得独占锁再锁资源，避免合并与发布交叉产生过期关联或锁顺序反转。

主分类必须是 `dimension=category` 且 `active`。一个资源至多一个主分类，放在投影列，不从多标签推断。

### 推荐位

`FeaturedSlot` 值：kind、placement（首版只有 `hero`）、position ≥ 1、资源 ID、左闭右开时间。无 `ends_at` 表示无限。重叠由数据库 GiST 拒绝，服务把排斥冲突翻译成 `invalid_argument`，字段 `starts_at`。

## 设计模式落点

| 模式 | 落点 |
| --- | --- |
| 聚合 | `Resource` 是唯一修改修订指针和版本的对象 |
| 值对象 | `Slug`、`ExternalURL`、`QualityScore`、`FieldPath` |
| 仓储 | `ResourceRepository`、`TagRepository`、`SlotRepository`，方法接受 `pgx.Tx` |
| 策略 | `Details` 的三种实现；`catalog/present` 按 kind 生成 `card` 文本 |
| 规约 | `PublishSpec.Check(resource, revision, tags)` |
| 乐观锁 | `edit_version` 比较，SQL 再加 `where edit_version = $expected` |
| 工作单元 | 最外层命令调用一次 `ports.Tx.Within`，本包 Tx 方法复用传入事务 |
| 门面 | `publication.Service` 实现 `ports.Publisher`，管理 HTTP 和以后的流水线都走它 |
| 工厂 | `SeedFactory` 把 JSON 变成 `NewDraft` 命令，不在导入命令里拼 SQL |
| 投影 | `Projector.Project(revision, tags) Publication` |

发布投影是同一数据库里的读模型，不是另一套 CQRS 基础设施。resource_publications 的写入统一归 P1 的 publication 投影模块：PublishTx 和位于该模块的 TagMerger 共用 ProjectionWriterTx；目录聚合本身、P3 查询、P6 采集与 P7 流水线都不直接写投影。

## 发布事务

`PublishTx(ctx, tx, cmd)` 使用调用方事务，不能自行 Begin、Commit 或调用 Within。HTTP 由 P2 幂等执行器持有事务，CLI 或后台由外层命令持有。进入资源锁前先取得 taxonomy 共享事务锁。`PublishSpec` 全部通过后才写：

1. `select ... for update` 锁定 `resources`。
2. `edit_version` 与命令一致，否则 `edit_conflict`。
3. 修订属于该资源，且 ID 等于当前草稿。
4. 标题、简介非空白；教程正文或步骤至少一项；标签引用先解析合并链，最终目标存在、active 且维度正确；主分类是 category；封面 URL 是站内路径 `/covers/...` 或 `https` 外链；质量分在范围内。
5. upsert `resource_publications`。`content_updated_at` 在投影的标题、简介、正文、标签、details、质量分、推荐理由有变化时更新，只改封面也算变化。
6. 删除并重插该资源的 `resource_tags`。
7. 首次发布写 `first_published_at`，以后不改。状态改 `published`。`draft_revision_id` 置空。`edit_version` 加 1。
8. `Auditor.Record`，action `publish`。
9. `RankingEnqueuer.Enqueue` 同一事务。
10. 返回结果给外层命令；外层提交成功后才能发出成功响应。

`search_text` 与 `card` 由 `internal/catalog/present` 的纯函数生成，P3 调用同一函数，避免两套中文标签和两套标准化。`search_text` 使用 NFKC、大小写折叠、压缩空白，拼接标题、别名、标准标签名、别名、简介、教程正文和步骤。P3 只读投影列，不另写规则。

`CreateDraftTx` 建资源及首份修订，`SaveDraftTx` 保存后续修订。两者返回新的资源版本和草稿 ID，不自行提交。保存草稿只修改修订、草稿指针、版本及审计，不调用 `RankingEnqueuer`，不写 `resource_tags`。同一事务继续 PublishTx 时使用 DraftResult 中的新 EditVersion 和 RevisionID。

来自 P7 的更新在 ReviewService 锁定资源后、调用 SaveDraftTx 之前，通过 HasUnpublishedDraft 检查现有指针与正式修订。为 true 则返回 draft_conflict，人工和自动采纳都不能替换草稿。管理员先处理草稿再生成建议。PublishTx 随后验证当前草稿等于本次 DraftResult，不把同一事务刚生成的候选草稿误判为既有草稿冲突。直接人工保存草稿仍按普通 edit_version 规则进行。

隐藏和归档只改 `resources.status` 与版本，保留投影。公开读由 P3 再查 `status = published` 且按配置排除 `is_demo`。

## 种子导入

`nexadm import --file src/data` 的最外层命令调用一次 Within，在一个事务里处理三个 JSON，所有 CreateDraftTx、SaveDraftTx、PublishTx 调用复用该事务。

| JSON | kind | slug | 身份键 | 时间 |
| --- | --- | --- | --- | --- |
| `tools.json` 的 `id` | tool | 原 `id` | `url:` + 规范化 `url` | `addedAt` 作为 `first_published_at` |
| `tutorials.json` | tutorial | 原 `id` | 空 | 同上 |
| `repos.json` | repo | 原 `id` | 空，直到有仓库数字 ID | 同上 |

映射：`name` 或 `title` → title，`desc` 或 `summary` → summary，`heat` 丢弃，`featured: true` 按 JSON 顺序写成该 kind 的 hero 推荐位。`freshness_eligible=false`，`is_demo=true`，`origin=import`。质量分 0。变更原因 `seed import`。

幂等：以 `(kind, slug)` 查找。已存在则比较 payload 规范化哈希，相同就跳过；不同则走 `SaveDraftTx` + `PublishTx`，不新建 ID。已有未发布人工草稿时停止并返回冲突，不由导入覆盖。推荐位以 `(kind, placement, position)` 的现有启用记录为准，重复执行不插入第二段无限区间。

生产环境 `NEX_ENVIRONMENT=production` 时命令拒绝执行，除非显式 `--allow-demo`。默认不在生产跑。

标签：种子里的中文标签全部建成 `capability`，slug 用一份写死在 `seed_tags.go` 的表，例如 `对话` → `chat`，`本地部署` → `local`。表里没有的标签命令失败并打印原词，不临时音译。主分类首版留空。

## HTTP 归属

P2 提供认证中间件之后，这些路由由 P1 注册：

| 方法与路径 | 行为 |
| --- | --- |
| `POST /api/admin/resources` | 新建草稿 |
| `PUT /api/admin/resources/{id}` | 保存草稿，体含 `edit_version` |
| `GET /api/admin/resources/{id}` | 管理详情，含草稿、正式修订号、锁、版本 |
| `GET /api/admin/resources/{id}/revisions` | 修订列表，不含行为数据 |
| `POST /api/admin/resources/{id}/publish` | `{ "edit_version", "revision_id" }` |
| `POST /api/admin/resources/{id}/visibility` | `{ "edit_version", "status", "reason" }` |
| `POST /api/admin/tags` `POST /api/admin/tags/{id}/merge` | 标签与合并 |
| `POST /api/admin/featured` `DELETE /api/admin/featured/{id}` | 推荐位；删除改为 `enabled=false` |

修改请求要求幂等键和 CSRF，GET 仅要求管理会话。P2 完成认证后，由幂等执行器调用一次 Within，给 P1 处理器的写回调显式传入 tx；回调不能提前写 HTTP。P1 的领域 Tx 方法不另外开启事务。

管理详情里的预览不走公开接口。`GET /api/admin/resources/{id}/preview` 返回与公开详情相同的 JSON 形状，数据来自草稿。没有管理会话时 401。

## 并发

两个请求同时保存：行锁串行化，后一个 `edit_version` 不符，返回 409。两个请求用同一新 slug：唯一约束使其中一个 409，`field_errors.field=slug`。同一 `(kind, identity_key)` 同样处理。

发布失败时投影、标签、版本、审计、River 任务一起回滚。

## 测试

单元测试不用数据库：状态机、字段锁并集、URL 规范化和私网拒绝、三种 Details、`PublishSpec`、搜索文本标准化、种子映射。

PostgreSQL 集成测试：

- 保存草稿后投影行不变。
- 发布提交后公开投影和标签可见；回滚夹具里人为失败时两者都不留。
- 把资源 A 的修订 ID 写进资源 B 的发布，复合外键拒绝。
- 并发保存只有一个成功。
- 同维度别名冲突被唯一约束拒绝。
- 标签合并无环。
- 合并两个各有标准名的标签后，目标仍只有一个 is_primary，旧标准名成为普通别名。
- 同时关联源和目标标签的资源合并后只有一条目标关系；主分类引用同步迁移。
- 标签合并后预览旧修订可解析到新标签，失败时字典、关系和投影共同回滚。
- 存在未发布人工草稿时，采集建议及种子刷新都不能替换草稿或发布它。
- HasUnpublishedDraft 的四种指针组合在管理查询、建议生成与锁内采纳中得到相同结果；指向已发布修订的残留指针不误报冲突。
- 外层事务在人为写入证据失败后回滚，先前的 SaveDraftTx、PublishTx、审计和任务均不存在。
- 推荐位时间重叠被 GiST 拒绝，首尾相接允许。
- 导入执行两次，资源数仍等于 JSON 条数。

## 完成定义

不启动采集器，管理员调用（测试里直接调服务）可以完成新建、发布、再草稿、下架。下架后的行仍在，公开与否留给 P3 的查询断言。`IdentityLookup.FindByIdentity` 对种子工具能命中。
