# P8 外部调用与预算技术方案

本包执行付费或可能付费的外部调用，并保证一笔调用的预占、回执和结算可以对账。第二阶段首批调用方是 P7。采集若以后接入付费网页服务，也走这里，不另写一套预算。

归属：`internal/providers`、`db/queries/provider.sql`。provider_calls、provider_budget_windows、provider_budget_reservations 由 P0 第二阶段 schema 基线创建；其中回执外键依赖同一基线里的 source_runs 和 processing_runs。基线先合并，P8 再独立合并实现及空库集成测试，不能只在第一阶段 15 张表上运行。

默认使用 DisabledClient，Complete 返回 model_disabled，不生成输出、不写成功回执、不预占费用。NEX_MODEL_ENABLED=true 时使用真实适配器。只有 development/test 显式 NEX_MODEL_FIXTURE=true 才绑定 FakeClient；生产 fixture 或两个开关同时为 true 都启动失败。

## 表结构增量

provider_calls 增加 `profile_version text`，允许非模型付费采集为空，processing_run_id 非空的模型调用必须填写。该字段与约束已同步数据库文档，由 P0 第二阶段基线创建，不在本包重复迁移。

## 功能点

- 统一的 `ports.ModelClient`。
- 一次逻辑请求一个 `request_key`。成功或结果不明时不允许再发出未决的第二次。
- 调用前在数据库里预占预算，调用后按实际费用结算。
- 超时且无法确认供应商结果时标记 `unknown`，保持预占，不自动重试。
- 明确失败才释放预占并允许下一尝试号。
- 模型流程测试显式注入假服务，另行覆盖 DisabledClient、fixture 配置限制和通过 Service 执行的预算结算测试。
- 预算用尽时返回 `budget_exhausted`，调用方把阶段标为 `blocked`。

不做：自动选择多家供应商比价、把未知调用偷偷再打一次、在公开接口里返回回执。

## 状态机

`provider_calls.status`：

```text
prepared -> sent -> succeeded
                 -> failed
                 -> unknown
```

prepared 表示额度已预占、请求还没发出。恢复扫描可以把超过 10 分钟仍处于 prepared 的行改为 failed，并在同一事务释放预占。发送方必须先执行带 status=prepared 条件的更新并提交，确认成功改为 sent 后才能发网络请求；若恢复已抢先改变状态，发送方放弃发送。这样恢复释放不会与迟到的发送方同时生效。

sent 超过该请求的超时与恢复宽限后仍无结果，恢复扫描标 unknown，不释放。管理员用 nexadm provider resolve 处理，必须提供 reason 和核对依据；确认成功还需 response-file、actual-cost、currency，确认未计费失败才允许释放。恢复命令复用同一结算事务，不能只更新状态。没有查询接口时由管理员核对，不自动再次调用模型。

尝试号从 1 增加。只有上一次是 `failed` 才允许 `attempt_no+1`。部分唯一索引挡住同一 `request_key` 上第二个 `prepared`、`sent`、`unknown` 或 `succeeded`。

## 预算对象

```go
type Window struct {
    scopeKey   string
    currency   string // 首版 CNY 或 USD，一个窗口一种
    start, end time.Time
    limit      decimal
    reserved   decimal
    spent      decimal
}

type Reservation struct {
    callID   uuid.UUID
    windowID uuid.UUID
    held     decimal
    settled  *decimal
    status   string // held, settled, released
}
```

金额用整数分或 `numeric`，Go 侧用 `shopspring/decimal`，不用 `float64`。

预占范围至少两扇窗口，按固定顺序加锁，避免死锁：

1. `provider:<providerKey>:day:<UTC日期>`
2. `task:<processingRunID>`，若有关联任务

`Authorize`：

```text
locked.spent + locked.reserved + estimate <= limit
```

否则整笔拒绝，不插入 sent。ModelRequest 的 ProcessingRunID 用于回执外键和任务预算，必须指向已持久化运行。P8 从不可变 ProfileVersion 解析供应商、模型、计费币种及价表，结合完整请求的保守输入 token 上限与 MaxOutputTokens 计算 estimate；调用方不能自行降低预占金额。缺失价格、未知 profile、币种与预算窗口不匹配时在发出前拒绝。输出上限必须传给供应商；真实费用仍超过估算时记账并记录 budget_overrun，不拒绝已经发生的费用。

成功结算与回执状态在一个事务：先锁 provider_calls，再按窗口 ID 排序锁定所有预算窗口和预占；保存 response_payload、用量和实际费用，reserved 减 held，spent 加实际费用，预占置 settled，最后将回执置 succeeded。同一 provider_call_id 重复结算只读取既有结果，不重复扣减。unknown 保持 held；明确未计费失败的状态与释放也同事务提交。

## 调用顺序

`Service.Complete`：

1. 若已有 succeeded 回执，确认其所有预算预占均已 settled 后返回保存的输出，不访问网络。若发现旧版本或人工修复留下的未结算成功行，先按已保存 actual_cost 幂等补结算；资料不足则告警并阻塞，不能直接成功返回。
2. 事务：插入 `prepared` 调用、预占额度。
3. 事务提交后，以 status=prepared 条件更新为 sent 并记录 sent_at；只有更新成功且提交已确认才能发网络请求。提交结果不明时先查库确认，不直接发送。
4. 调用供应商。
5. 成功：使用上述同一结算事务保存响应、用量、实际费用并完成预算结算，最后置 succeeded。提交后才向调用方返回。禁止先单独提交 succeeded 再另行结算。后续 P7 失败不撤销已确认发生的费用，重跑复用回执。
6. 明确的 4xx 参数错误：`failed`，释放预占，不重试。
7. 429 或网络断开在**发出后**无法区分是否到达：标 `unknown`。只有在连接还没建立时的 DNS 失败可以标 `failed`。
8. 可重试的 `failed` 由 River 安排下一次尝试，受阶段的尝试上限约束。默认模型阶段最多 2 次，且第二次必须是新的 `attempt_no`。

第 3 步和第 4 步之间崩溃会留下 `sent`。恢复任务把它改成 `unknown`。这是故意的，避免把可能已经计费的请求再发一次。

供应商响应已收到、结算事务尚未提交时崩溃，数据库仍为 sent 且保持预占，恢复后按 unknown 核对；提交后崩溃则回执和费用均已完成，可安全重放。技术错误不能将这种已发送请求标成可再次发送的 failed。

## 供应商适配

```go
type ChatProvider interface {
    Key() string
    Send(ctx context.Context, req ModelRequest) (ProviderResult, error)
}
```

`ProviderResult` 含供应商请求 ID、输出 JSON、输入与输出 token、实际费用、币种。适配器把供应商字段翻译成这个结构。`Service` 不出现某一家的 JSON。

客户端模式明确分为三种：

| 类型 | 行为 |
| --- | --- |
| `DisabledClient` | 默认模式，返回 model_disabled，不生成业务输出 |
| `FakeClient` | 仅测试注入或 development/test 显式 fixture；按 SchemaName 读夹具，响应 Mode=fixture、ProviderCallID=nil，不写真实付费回执 |
| `OpenAICompatible` | 仅当 `NEX_MODEL_ENABLED=true` 且基址、模型名、密钥环境变量都存在时注册 |

密钥只从 `credential_ref` 指向的环境变量读取。日志里的 URL 去掉查询串，正文替换为长度。`response_payload` 可以存模型输出，因为审核需要它；不存请求头。

FakeClient 是显式测试替身，校验与真实服务相同的 ModelRequest 必填字段，尤其是 ProcessingRunID、RequestKey、SchemaName、ProfileVersion 和输出上限。预算集成测试使用 Service 加 fake ChatProvider，让 Service 正常写回执和预算；不能用绕过 Service 的 FakeClient 宣称预算测试通过。

## 和流水线的接口

P7 只依赖总契约定义的 ModelRequest、ModelResponse 和下面的端口；此处不另增 estimate 等请求字段：

```go
type ModelClient interface {
    Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}
```

ModelResponse.Output 是 JSON，live 响应必须有已完成结算的 ProviderCallID。错误分别使用 model_disabled、budget_exhausted、provider_unknown；P7 持久化 blocked 状态，不交给普通重试路径立即重发。管理员恢复任务时仍先检查回执。

## 管理查询

`GET /api/admin/provider-calls?status=unknown` 只给管理员，返回 ID、供应商、状态、预占、时间，不返回完整提示词。处理未知调用用 `nexadm`，避免在界面上做一键重试。

## 测试

- 成功路径：窗口 `reserved` 回落后 `spent` 增加一次；第二次 `Complete` 不增加 `spent`，且 fake 计数器不再加。
- 并发两个相同 `request_key`：只有一个 `sent`。
- `unknown` 之后再次 `Complete` 返回 `provider_unknown`，不插入新的 `sent`。
- 明确失败后 `attempt_no=2` 可以发送。
- 预占超过日限额时不插入 `sent`，窗口金额不变。
- 结算执行两次，`spent` 只加一次。
- `prepared` 超时被恢复任务改成 `failed` 并释放。
- `sent` 超时被改成 `unknown` 且 `reserved` 仍包含该笔。
- NEX_MODEL_ENABLED 为空且没有显式 fixture 时，构造 DisabledClient，不返回任何加工文本。
- production 加 fixture 启动失败；development/test 显式 fixture 才使用 FakeClient，两个开关同开也失败。
- 使用真实 ModelRequest 创建有外键的回执及对应币种预算；缺 ProcessingRunID、未知 profile 或非法输出上限时不发网络请求。
- 在响应收到后、结算事务各写入点和提交后注入故障；不能出现 succeeded 与 held 同时存在，提交前失败保持待核对，提交后重放不重复扣费。
- 恢复扫描与迟到发送方并发时，只有成功把 prepared 改成 sent 的请求可发送。

## 完成定义

P7 的测试不用改就能把假的 `ModelClient` 换成这个服务的假适配器。打开真实供应商之前，未知调用不会被自动重试，预算窗口能对上预占和实扣。
