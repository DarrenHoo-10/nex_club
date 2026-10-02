# P5 管理前端技术方案

本包是同一套 Vite 应用里的 `/admin` 界面，供一名管理员完成录入、改草稿、预览、发布和下架。它调用 P1 的管理接口和 P2 的会话接口，不直接改数据库，也不在浏览器里拼发布事务。

公开页面的组件可以复用。管理请求不进 `src/api/client.js` 的公开客户端，单独放在 `src/admin/api.js`，避免公开页面带上 CSRF 逻辑。

## 功能点

- 登录、退出、刷新后会话仍在。
- 资源列表：按 kind、状态、标题关键词过滤。
- 新建三种资源，保存草稿，看到版本冲突。
- 预览草稿：工具和仓库用 `Card`，教程用 `Reader`。
- 发布当前草稿，隐藏、归档、恢复。
- 修订历史只读，按字段看上一版和这一版的差异。
- 标签列表、新建、合并。
- 推荐位表格：板块、位置、资源 slug、开始和结束时间。

不做：富文本编辑器、多人同时编辑的光标、信源和审核队列（第二阶段再加页面）、角色管理。

## 路由

```text
/admin/login
/admin/resources
/admin/resources/new?kind=tool
/admin/resources/:id
/admin/tags
/admin/featured
```

`/admin` 重定向到资源列表。未登录访问这些页面时转到登录，并带 `next`。已登录访问登录页时转到 `next` 或列表。

路由组件用 `React.lazy` 放在 `src/admin/`，公开首页不静态导入它们。

## 会话处理

`src/admin/session.js`：

- `login(username, password)` 调用 `POST /api/admin/session`，`credentials: 'include'`。
- CSRF 从 `nex_csrf` Cookie 读取，写请求放进 `X-CSRF-Token`。
- 每个写请求生成新的 `Idempotency-Key`（`crypto.randomUUID()`）。用户在 409 `edit_conflict` 之后手动再次保存，使用新键和新的 `edit_version`。同一按钮在网络超时后的自动重试复用旧键和旧体。
- 401 清空内存中的管理员并跳转登录。403 CSRF 提示刷新页面。

自动重试只做一次，只针对网络失败和 503，不对 400 和 409 重试。

## 编辑器模型

浏览器里的草稿是一个普通对象，字段与 `PUT` 体一致。它不是领域聚合，校验提示来自服务端 `field_errors`。本地只做「必填为空则禁用保存」这种体验，不复制 slug 正则以外的规则。slug 正则可以在输入时提示，服务端仍是最终裁决。

```js
{
  editVersion,
  kind,
  slug,
  title,
  aliasesText,          // 编辑时一行一个，提交时拆成数组
  summary,
  bodyMarkdown,
  coverUrlsText,
  primaryCategoryId,
  tagIds,
  qualityScore,
  recommendationReason,
  details,               // 按 kind 分表单
  unlockFields,          // 本次显式解锁的路径
  changeReason,
}
```

`first_published_at` 一旦存在，slug 输入框只读。界面说明 slug 在发布后保持不变。

三种 details 表单：

| kind | 字段 |
| --- | --- |
| tool | 官网、收费下拉、平台多选、部署多选 |
| tutorial | 难度、分钟、Markdown 正文、步骤列表（可排序）、作者、原文链接、注意；正文和步骤至少一项 |
| repo | owner/name、语言、许可证、是否归档、最近活动时间；仓库数字 ID 只读，空时显示「等待采集确认」 |

字段锁从管理详情的 `field_locks` 显示为小锁。默认不解锁。管理员勾选「允许流水线以后改这个字段」才把路径放进 `unlockFields`。

保存成功后用响应里的新 `edit_version` 替换本地版本。响应 409 时展示「内容已更新」，按钮「加载服务器版本」重新 GET，不自动覆盖本地未保存输入。未保存输入留在内存里，直到离开页面前 `window.confirm`。

发布按钮调用 `POST .../publish`，体是当前 `edit_version` 和当前草稿修订 ID。成功后跳到公开页 `/resources/:slug` 以便核对。公开页若因演示数据在生产被隐藏，管理端改为打开预览接口的结果。

预览：`GET /api/admin/resources/{id}/preview` 的 JSON 交给 `src/api/view.js`，同一 Card / Reader。教程必须传 body_markdown、details.steps 和 notes，并复用 P4 的 Markdown 安全渲染规则。预览区标明“未发布”。P4 尚未合并时，映射先写在 src/admin/view.js，P4 合并后改为从 src/api/view.js 导入，不保留两份。

## 列表与其他页

资源列表列：标题、kind、状态、版本、有无未发布草稿、更新时间。行点击进入编辑。状态筛选是查询串 `status`，刷新可恢复。

修订页：选择两个版本号，前端按字段做浅比较。`details` 按键比较。不实现通用 JSON patch 库。

标签页：新建名称、维度、slug。合并时选择目标标签，提交前确认「正式内容上的旧标签会改挂到目标」。

推荐位页：表单提交后，409 或 400 且字段是 `starts_at` 时显示「这个位置在该时间段已经有推荐」。结束时间可空。

## 设计约束

- 管理页面的样式沿用现有 `glass` 与字体，不另做一套设计系统。表单用普通标签和控件，保证键盘可操作。
- 组件不调用 `fetch`。页面调用 `src/admin/api.js`。
- 不在 `localStorage` 存口令或 CSRF。
- 发布、下架、合并标签这些不可轻易撤销的动作使用 `<dialog>` 确认，说明后果。

这是**分层**：页面是适配器，API 模块是网关，展示复用公开组件。没有把管理状态放进公开 `App` 的 `useState`。

## 测试

Vitest：

- 未登录渲染 `/admin/resources` 时导航到登录。
- 保存时请求头带 CSRF 和幂等键；超时重试使用同一键。
- 409 后不清除文本框。
- 教程预览把 `details.steps` 交给 `Reader`。
- 仅有 Markdown 正文的教程在编辑预览和发布后详情中均完整显示；危险 HTML 与链接不执行。
- 发布后 slug 输入为只读。

夹具实现 `src/admin/fixture.js`，在没有后端时走通新建到发布按钮。P1、P2 合并后再做一次手工验收：登录、录入一条工具、公开页能看见、隐藏后公开页 404、错误口令不创建会话。

## 完成定义

一名管理员只通过浏览器完成三种资源的草稿、预览、发布、隐藏和标签合并。公开构建里，未访问 `/admin` 时不加载管理表单模块。
