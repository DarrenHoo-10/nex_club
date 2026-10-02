# 字段与操作

工具名可能带宿主的 MCP 命名空间前缀，以实际发现的 `nex_club` 工具为准。

## 通用待执行操作

`prepare_action` 参数：`operation`、可选目标 `id`、完整 `body`、每个意图唯一的 `request_key`（建议 UUID）。结果包含操作 ID、`preview`、`preview_digest`、24 小时有效期、后台 `preview_url`。

`execute_action` 参数：`id`、`preview_digest`，可选 `reject: true` 取消操作。需要执行权限；服务端核对操作者、摘要、有效期和资源版本，执行与结果、审计同事务提交。

## 新建草稿

`operation=create_draft`，不传目标 id。例子：

```json
{
  "kind": "tutorial",
  "slug": "ai-reading-notes",
  "title": "AI 辅助整理阅读笔记",
  "summary": "记录来源、区分事实与判断，并人工核对摘要。",
  "body_markdown": "## 整理步骤\n\n先记录原文链接，再把事实和个人判断分开，最后逐项人工核对。",
  "aliases": [],
  "cover_urls": [],
  "primary_category_id": null,
  "tag_ids": [],
  "quality_score": 50,
  "recommendation_reason": null,
  "details": {
    "level": "beginner",
    "minutes": 3,
    "steps": ["记录原文链接", "区分事实与判断", "人工核对摘要"],
    "source_url": null,
    "author": "",
    "notes": "基于用户提供的经验整理"
  },
  "change_reason": "用户要求生成草稿"
}
```

`slug` 为稳定的小写短标识。`quality_score` 0..100。`body_markdown` 必须使用实际换行，不要生成双重转义的反斜杠 n。分类和标签必须使用 `list_tags` 返回的 UUID。

类型专属字段：

| 类型 | details |
| --- | --- |
| `tutorial` | `level` 为 unknown/beginner/advanced；`minutes` 为非负整数；`steps` **是字符串数组**；无原文时 `source_url=null`；可含 author、notes。须有正文或步骤。 |
| `tool` | `website_url` 为官方网站；`pricing` 为 unknown/free/paid/freemium；`platforms` 可选 web/desktop/mobile/api/plugin；`deployment` 可选 hosted/self_host/local。 |
| `repo` | `github_repository_id` 为真实平台 ID 字符串；`full_name` 为 owner/name；可含 language、license、archived、last_activity_at。不能用编造 ID 保存。 |

## 修改草稿和标签

`operation=save_draft`，`id` 为资源 UUID。先取 `get_resource` 的 draft 或 published 全部字段，保留原有 null 和数组，再修改目标字段；`body.edit_version` 取资源当前版本，提供 `change_reason`。完整正文、分类和推荐理由都必须保留，不能仅传一个 summary 当作局部补丁。`unlock_fields` 不支持，请用户在资源编辑器明确解锁。

## 发布、下架、恢复

- `publish`：目标资源 id；body 为 `{"edit_version":当前版本,"revision_id":"当前 draft_revision_id"}`。
- `hide` / `show`：目标资源 id；body 为 `{"edit_version":当前版本,"reason":"具体原因"}`。

先准备并展示预览，再依据用户对这份具体内容的授权执行。

## 采集与加工

- `run_source`：目标信源 id；body 为 `{"edit_version":信源当前版本}`。只支持已启用且允许主动抓取的信源。
- `retry_processing`：目标加工阶段 id；body 为 `{}`。只允许失败/阻塞、无活跃队列任务、无结果未知付费调用且原计划可用的阶段。

查询：`automation_status(view=overview|sources|tasks|proposals)`；`read_material(id或q)`；`list_resources(q,kind,status)`。
