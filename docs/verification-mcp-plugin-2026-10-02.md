# MCP 插件验收（2026-10-02）

最终形态为 Nex Club 插件（MCP + Skill），无内嵌聊天页或聊天 API。插件已注册为本地市场的可用条目，尚未安装或配置正式访问令牌。

## 实际运行

本机真实 API `http://localhost:8089` 与 PostgreSQL 17，服务使用受限运行角色。直接通过 Streamable HTTP MCP 请求验证：

- initialize 协商 2025-11-25，tools/list 根据只读、准备、执行三种权限返回工具。
- 无令牌返回 401，非允许 Origin 返回 403；只读不能准备，准备权限不能执行。
- 原始素材查询返回 8 条，回环地址读取被拦截。
- prepare_action 创建待确认操作，资源总数不变；错误摘要被拒绝。
- execute_action 创建草稿，公开详情仍为 404；重复执行返回同一资源和修订。
- 准备发布后再次修改草稿，旧发布执行返回 edit_conflict，操作保持 pending，公开内容不变。
- 新版本发布成功，公开详情为 200；验收后下架，公开详情恢复 404。
- 所有验收令牌已撤销，撤销后 tools/list 返回 401。
- 旧聊天 API 返回 404。早期聊天试运行的调用账目保留，MCP 路径本身不调用模型。

验收资源 `21e9e5da-2cfc-42e9-ad44-25887290950b` 最终为 hidden，未留下公开测试文章。工具记录保留在后台供核查。后续以只读方式验证了指定操作 API、预览链接和详情预览弹窗。

## 自动检查

- 前端 54 项测试通过，覆盖预览后确认与令牌默认只读。
- 全量 Go 检查通过，使用独立 nex_tests 数据库，包含 MCP 官方客户端连接、权限、撤销、摘要约束、幂等和冲突回滚。
- 前端生产构建通过；OpenAPI YAML 解析通过；git diff --check 通过。
- Skill Creator 校验通过；Codex plugin list 可发现 nex-club@nex-club-local，版本 0.1.0，installed=false。
- 插件 ZIP 包含 5 个文件；配置仅含令牌环境变量名，不含令牌或 CPA 凭据。

未声称验证：插件安装后的新 Codex 会话自动选用 Skill、OAuth 客户端、公共插件商店发布、远端 HTTPS 部署。当前交付为可安装的本地 Codex 插件。
