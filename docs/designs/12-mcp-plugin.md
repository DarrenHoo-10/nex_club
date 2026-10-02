# Nex Club 内容管理插件

## 边界

聊天、讨论、文章生成由 Codex 等宿主客户端提供。Nex Club 不维护独立聊天界面、聊天历史或专用聊天模型调用链。自动采集加工仍使用原有 CPA 配置和预算服务。

仓库 `plugins/nex-club` 包含 Codex 兼容插件清单、MCP 连接配置及 `manage-content` Skill；`.agents/plugins/marketplace.json` 提供本地安装来源。当前为个人本地插件，未安装到用户客户端，也未提交公共插件商店。

`/mcp` 使用官方 Go MCP SDK 1.7.0、无状态 Streamable HTTP。SDK 要求 Go 1.25，项目与 CI 同步升级。浏览器后台使用既有会话与 CSRF；MCP 每次请求独立验证 Bearer 令牌，不接受浏览器 Cookie 替代。

## 权限与协议

令牌为 256 位随机值，只返回一次，数据库仅存 SHA-256 摘要，30 天过期。关联管理员被禁用或令牌被撤销后，后续 MCP 请求立即拒绝。Origin 存在时必须匹配配置的公开站点。

权限分层：

| 权限 | 工具 |
| --- | --- |
| 只读（默认） | list_resources、get_resource、list_tags、automation_status、read_material、read_url、get_action |
| 准备操作 | 增加 prepare_action |
| 执行操作（需要同时具有准备权限） | 增加 execute_action |

所有令牌只能查看和执行其所属管理员的待执行操作。管理员对资源仍具有既有后台权限；本版不引入租户或文章级 ACL。外部网页读取复用防 SSRF 客户端，限制 HTTP(S)、大小、超时及重定向，拒绝私网/回环地址，不携带后台凭据。

## 执行流程

1. MCP 读取当前资源和版本。Skill 与用户讨论内容，按用户授权决定是否准备写操作。
2. prepare_action 用调用方的 request_key 幂等登记不可变命令。先在只回滚的事务中执行相同领域命令做校验和效果预览；不提交资源、发布、队列任务或领域审计。
3. 返回待执行操作、24 小时有效期、preview、preview_digest、preview_url。摘要由规范化后的命令和预览计算；它是内容绑定校验，不是授权凭证或人类确认的证明。
4. 客户端展示具体内容与变更。根据用户对该操作的授权调用 execute_action；发布、下架等缺少明确授权时，Skill 在预览完成后请求确认。插件默认对执行工具使用宿主确认策略。有准备权限但无执行权限时，可在后台「MCP 接入 → 操作预览与记录」确认。
5. 最外层 WriteExecutor 持有幂等记录，锁定操作行，校验摘要、有效期和资源版本，在同一事务内执行领域命令、保存结果并追加审计。版本冲突全部回滚，操作仍为 pending；重新读取、准备、展示后才继续。
6. 重复执行重放既有结果。超时先 get_action，不通过创建另一份操作绕过不明状态。

支持 create_draft、save_draft（含标签）、publish、hide、show、run_source、retry_processing。更新必须提交完整快照；字段解锁、信源编辑、加工建议的逐字段采纳、模型预算配置继续在已有后台完成。

## 确认边界

持有“执行”令牌的客户端具有写权限；服务端不能判断客户端是否真的向人类展示了预览。Skill 和宿主工具确认策略负责用户意图，服务端负责权限、命令内容绑定、并发控制与审计。需要服务端强制人工点击时，只发放“准备”令牌，不发放执行权限。

## 数据与兼容

00006 在内嵌聊天试运行中已经应用。00007 将 assistant_actions 改名为 mcp_actions，增加令牌执行权限。已发生的模型调用、预算回执及其关联的试运行对话行作为历史账目保留，不再有创建聊天或调用聊天模型的接口。不能通过回写已应用迁移删除这些费用记录。

后台 API：GET/POST /api/admin/mcp/tokens；DELETE /api/admin/mcp/tokens/{id}；GET /api/admin/mcp/actions 与 /{id}；POST /api/admin/mcp/actions/{id}/confirm（preview_digest、reject，带 CSRF 与 Idempotency-Key）。

本地连接由 `NEX_CLUB_MCP_TOKEN` 注入凭据；远端部署应将插件 URL 设为实例的 HTTPS /mcp。本版不实现 OAuth，因此不能声称可直接用于只接受 OAuth 的公共连接器。
