# Nex Club

AI 工具网站、教程与 GitHub 项目目录。前端使用 Vite + React，后端使用 Go、PostgreSQL 与 River。

## 项目方案

[Go 后端实施方案](docs/backend-implementation-plan.md)覆盖内容服务、数据模型、排序搜索、管理后台，以及参考 AIHOT 的采集加工流程和分阶段验收。

[数据库表结构设计](docs/database-schema.md)细化两阶段的字段、主外键、索引、关系图、发布事务与建表顺序。

[并行工作拆分与技术方案](docs/designs/00-split-and-contracts.md)把上述草案拆成 P0–P8。总契约冻结 JSON 和跨包端口，各模块文档写功能点、领域对象、设计模式和事务步骤，便于同时开工。

[代码修复与实际验收记录](docs/verification-2026-10-01.md)记录本轮修复、真实 CPA 调用和浏览器验收结果。

```bash
npm install
npm run dev     # http://localhost:5173
npm run build   # 产物在 dist/
```

正常运行时页面读取 Go API。`src/data/` 下的 JSON 仅用于开发种子导入：

- `tools.json`：AI 工具网站
- `tutorials.json`：AI 焚决（教程，含步骤与提示）
- `repos.json`：GitHub 项目

开发种子使用 `nexadm import --file src/data` 导入；正式内容通过管理后台或采集审核发布。

## 启动后端

先启动 PostgreSQL，并准备两个独立数据库凭据：迁移账号拥有建表权限，运行账号仅继承 `nex_app`，不得是数据库或表所有者。

```bash
cd server
go build -o bin/nexadm ./cmd/nexadm
go build -o bin/api ./cmd/api
go build -o bin/worker ./cmd/worker
# 仅在迁移命令环境中设置 NEX_MIGRATION_DATABASE_URL。
./bin/nexadm migrate
```

迁移命令同时执行 River 迁移并授予 `nex_app` 必需权限。数据库管理员另行创建运行用 LOGIN 角色、设置随机口令，再执行 `GRANT nex_app TO <运行角色>`。配置运行进程的 `NEX_DATABASE_URL` 指向该角色；API 和 worker 启动时不会执行 DDL，并会拒绝能修改审计或创建 schema 对象的账号。

按 `.env.example` 设置运行配置，启动 `server/bin/api` 和 `server/bin/worker`。开发前端通过 Vite 代理 `/api` 到 8080；也可先 `npm run build`，设置 `NEX_STATIC_DIR` 为 dist 的绝对路径，由 Go 提供页面。

管理员初始化使用 `NEX_ADMIN_USERNAME`、`NEX_ADMIN_PASSWORD` 和运行数据库凭据执行 `nexadm admin init`。种子导入和管理员初始化都不需要迁移账号。

## CPA 模型配置

使用 OpenAI 兼容的 chat/completions 接口。配置 CPA 的 base URL（包含 `/v1`）、访问密钥与 `NEX_MODEL_NAME=gemini-3.8-flash-high`，然后显式开启 `NEX_MODEL_ENABLED=true`。费用窗口和 token 费率通过 `NEX_MODEL_DAILY_LIMIT`、`NEX_MODEL_CURRENCY`、`NEX_MODEL_INPUT_PER_TOKEN`、`NEX_MODEL_OUTPUT_PER_TOKEN` 配置；服务未返回费用时，回执金额按这些费率计算，供应商账单另行核对。

模型配置版本由 endpoint、模型和价表内容生成，旧轮次遇到不可用配置时阻塞，不把新配置重新登记成旧版本。夹具模式只允许在 development/test 显式开启 `NEX_MODEL_FIXTURE=true`，与真实调用互斥。

凭据放在忽略的本地环境文件中，不提交。`.env.cpa.local` 可存运行配置；迁移凭据使用另一个仅供迁移命令的环境。

## 列表排序

三个板块支持「推荐 / 热度 / 最新」，可与搜索和标签筛选组合使用。真实接口按排名快照排序；关键词搜索默认按相关度。切换板块保留排序偏好。

- `heat`：非负热度分数，按从高到低排序；未填写时按 0 处理。
- `addedAt`：收录时间，使用 ISO 8601 格式（例如 `2026-09-30T12:00:00Z`），按从新到旧排序；未填写或无效时排在末尾。

上述字段属于开发 JSON；示例热度不会导入为真实行为统计。正式内容的收录时间来自发布事务，热度来自服务端统计，平分使用稳定排序键。

卡片尾行不足一行时靠左对齐，手机端仍为单列。

## 封面（多张）

每个条目的封面是一个可切换的图集。默认自动生成 3 张示意封面；想用真实截图，在条目里加 `covers`：

```json
{ "id": "claude", "name": "Claude", "covers": ["/covers/claude-1.png", "/covers/claude-2.png"] }
```

图片放在 `public/covers/` 下即可，张数不限。也可用 `coverCount` 调整生成封面的数量。

## 内容管理插件

独立插件仓库：[nex-club-plugin](https://github.com/DarrenHoo-10/nex-club-plugin)。本仓库保留开发副本 [`plugins/nex-club`](plugins/nex-club/README.md)，将 MCP 与 `manage-content` Skill 打包，直接在 Codex 对话中研究素材、整理草稿、预览及执行发布/采集工作流。后台入口为 `/admin/automation/mcp`，用于令牌管理与操作检查，不再提供独立 AI 聊天页。MCP 地址为 `/mcp`；权限、确认和幂等契约见 [设计说明](docs/designs/12-mcp-plugin.md)。
