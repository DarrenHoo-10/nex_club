# Nex Club

AI 工具网站 · AI焚决集合 · 有价值的 AI GitHub 项目 的前端原型（Vite + React，中文界面）。

## 项目方案

[Go 后端实施方案](docs/backend-implementation-plan.md)覆盖内容服务、数据模型、排序搜索、管理后台，以及参考 AIHOT 的采集加工流程和分阶段验收。该文档为实施草案，当前项目仍为前端原型。

[数据库表结构设计](docs/database-schema.md)细化两阶段的字段、主外键、索引、关系图、发布事务与建表顺序。

```bash
npm install
npm run dev     # http://localhost:5173
npm run build   # 产物在 dist/
```

内容全部来自 `src/data/` 下的 JSON：

- `tools.json`：AI 工具网站
- `tutorials.json`：AI 焚决（教程，含步骤与提示）
- `repos.json`：GitHub 项目

新增条目只需往对应 JSON 数组里追加一项，页面会自动出现，并参与搜索与标签筛选。

## 列表排序

三个板块均支持「推荐 / 热度 / 最新」，可与搜索和标签筛选组合使用。默认「推荐」保留 JSON 数组中的编辑顺序，切换板块时保留当前排序方式。

- `heat`：非负热度分数，按从高到低排序；未填写时按 0 处理。
- `addedAt`：收录时间，使用 ISO 8601 格式（例如 `2026-09-30T12:00:00Z`），按从新到旧排序；未填写或无效时排在末尾。

当前热度分数与收录时间均为原型示例数据，不代表真实访问量、GitHub Star 数或产品发布日期。相同分数或时间保留原有编辑顺序，接入真实数据后可替换这两个字段。

卡片尾行不足一行时靠左对齐，手机端仍为单列。

## 封面（多张）

每个条目的封面是一个可切换的图集。默认自动生成 3 张示意封面；想用真实截图，在条目里加 `covers`：

```json
{ "id": "claude", "name": "Claude", "covers": ["/covers/claude-1.png", "/covers/claude-2.png"] }
```

图片放在 `public/covers/` 下即可，张数不限。也可用 `coverCount` 调整生成封面的数量。
