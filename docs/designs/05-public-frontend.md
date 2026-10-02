# P4 公开前端技术方案

本包把现有 Vite + React 页面从 `src/data/*.json` 改为读取公开 API，并换成真实路径。视觉组件 `Card`、`Gallery`、`Carousel`、`Reader`、`Cover`、`Logo` 继续负责展示。请求、排序和路由不写进这些组件。

不实现：热度公式、管理页面、服务端渲染。

## 功能点

- `/tools`、`/tutorials`、`/repos` 三个板块。
- `/resources/:slug` 详情。教程在详情页使用现有阅读布局；工具和仓库展示简介、标签、主链接和推荐理由。
- 搜索、标签、排序、游标进入查询串，刷新后保持。
- 切换板块清空 `q` 和 `tag`，保留 `sort`。从推荐降级来的 `effective_sort` 要显示出来。
- 首次加载、空结果、错误重试、下架后的不可用状态。
- 快速改关键词时丢弃过期响应。
- 详情打开和外链点击上报事件，失败不影响阅读。
- 旧地址 `#tools`、`#tutorials`、`#repos` 进入对应新路径一次。

第一阶段列表用「加载更多」，不用无限滚动。`total` 为 `null` 时标题沿用「全部 AI 工具网站」这种板块名，不显示 0。

## 路由与状态

增加依赖 `react-router-dom` 6。页面状态以 URL 为来源，不另建一套会和地址栏分叉的全局 store。

```text
/tools?q=&tag=&sort=&cursor=
/tutorials?...
/repos?...
/resources/:slug
```

`sort` 缺省不写进地址。有 `q` 时界面增加「相关度」。按钮高亮使用响应里的 `effective_sort`，不用本地猜测。用户点击排序才写入 `sort`。

`cursor` 不放进地址栏。加载更多把 `next_cursor` 留在组件状态里，避免分享一个中间页。刷新回到第一页，这是明确选择。

板块常量仍是一处：

```js
export const SECTIONS = [
  { key: 'tools', kind: 'tool', label: 'AI工具网站', title: '...', blurb: '...' },
  { key: 'tutorials', kind: 'tutorial', label: 'AI焚决集合', title: '...', blurb: '...' },
  { key: 'repos', kind: 'repo', label: 'AI GitHub 项目', title: '...', blurb: '...' },
]
```

`kind` 使用 API 枚举。现有组件的 `site` / `doc` / `repo` 只留在 `toGalleryKind()`，不扩散到请求层。

## 请求层

`src/api/client.js`：

```js
export function createClient({ baseUrl, fetchImpl }) {
  return {
    listResources(params, { signal }) {},
    getBySlug(slug, { signal }) {},
    listTags(params, { signal }) {},
    listFeatured(kind, { signal }) {},
    postEvents(events) {},
  }
}
```

`baseUrl` 默认空字符串，开发时由 Vite 代理 `/api` 到 `127.0.0.1:8080`。类型来自 `openapi-typescript` 生成的 `src/api/schema.d.ts`，手写客户端只做 `fetch`、查询串和错误。

错误是 `ApiError { status, code, message, requestId }`。网络失败 `code=network`。JSON 不符合契约 `code=bad_response`。

`listResources` 使用调用方传入的 `AbortSignal`。`useResourceList` 在参数变化时 abort 上一次。响应对不上当前参数序号就丢弃。这是请求层的**防过期**，不是组件里散落的标志位。

事件 `postEvents` 不接收 signal 取消后的重试风暴：失败就放弃，页面不提示。`event.id` 用 `crypto.randomUUID()`，同一次点击重试时复用同一个 ID。

开发没有 API 时，`VITE_API_MODE=fixture` 使用 `src/api/fixture.js` 读取 OpenAPI 示例返回三张卡片。默认 `live`。夹具模式让 P4 不阻塞 P3。

## 视图模型适配

`src/api/view.js` 把 `ResourceCard` 转成现有组件能吃的对象：

```js
{
  id, title, desc: summary, tags: tags.map(t => t.name),
  covers: cover_urls, coverCount: cover_fallback_count,
  subtitle: card.subtitle, meta: card.meta, href: card.href, cta: card.cta,
}
```

`Card` 继续接收 `title`、`sub`、`desc`、`meta`、`href`、`cta`、`onTag`。`onTag` 写入查询串里的标签 slug，展示仍用中文名。标签按钮的标识是 slug，避免同名混淆。

`Gallery` 不改生成封面的逻辑：`covers` 为空时按 `coverCount` 生成。

`Reader` 改为接收视图对象 `{ title, levelLabel, minutes, bodyMarkdown, steps, notes }`。bodyMarkdown 来自 body_markdown，steps 来自 details.steps，缺省分别为 null 和空数组。正文非空时先渲染正文，步骤非空时再渲染步骤；只有正文、只有步骤或两者同时存在都必须完整展示。详情路由、板块内打开和管理预览共用同一对象，不再直接传 JSON 原条目。

Markdown 使用声明的解析组件渲染，首版禁用原始 HTML，不直接注入未经处理的 HTML；链接和图片 URL 按白名单协议处理，拒绝 javascript、data 等危险协议。模型来源与人工来源采用相同规则。解析失败显示可读纯文本及提示，不静默显示空内容。

详情页：

- 工具、仓库：封面、标题、简介、标签、主按钮（`card.href`）、可选推荐理由。
- 教程：同一套 `Reader` 结构放在页面主体里，不再只是浮层。板块列表点击教程进入 `/resources/:slug`。
- 404 或 `code=not_found`：显示「这份内容已经下架或不存在」，保留回到板块的链接。

外链点击在 `window.open` 之前调用 `postEvents(outbound_click)`。详情页挂载时发 `detail_view`。严格模式双调用由事件 ID 与服务端去重吸收；开发时用 `useRef` 保证同一次挂载只发一次，避免依赖去重掩盖错误。

## 交互细节

- `/` 聚焦当前页搜索框，输入框已经聚焦时不拦截。
- 切换板块用 `<Link>`，不用 `location.hash`。
- 加载中保留上一屏数据并标记 `aria-busy`，避免闪空。
- 错误态有「重试」按钮，重新请求当前查询。
- 空结果沿用「没有找到匹配的内容，换个关键词试试。」
- 推荐位只在没有 `q` 和 `tag` 时显示，数据来自 `listFeatured`，不再用 JSON 的 `featured`。
- 手机单列和桌面网格保持现有样式。本包不改视觉语言。

静态资源仍由 Vite 构建进 `dist/`。P0 的 API 进程在 P4 合并时增加文件服务：`/`、`/tools`、`/tutorials`、`/repos`、`/resources/` 回 `index.html`；`/api/` 不回退；`/assets/` 长缓存。这个文件服务是 `internal/httpapi/static` 里很少的代码，由 P4 提交，避免 P0 提前绑定前端路由。

## 测试

没有浏览器自动化依赖时，用 Vitest 加 React Testing Library 测：

- 夹具模式下三个路径渲染标题和卡片。
- 输入关键词后请求带 `q`，慢响应先返回被丢弃。
- `effective_sort=latest` 时「最新」按钮为按下状态。
- `#tools` 渲染后地址变为 `/tools`。
- 详情 404 显示下架文案。
- body_markdown 非空而 details.steps=[] 的教程显示完整正文；只有步骤与正文加步骤也有用例。
- Markdown 中的原始 HTML、事件属性和 javascript 链接不能成为可执行内容，正常 HTTPS 链接可用。
- 点击标签写入 `tag` 查询串。

组件测试不发真实网络。另有一份手工验收清单写在本文件末尾，等 API 联调时走一遍手机宽度和桌面宽度。

## 手工验收

- 三个板块搜索、单标签、推荐/热度/最新可以组合，刷新后还在。
- 加载更多不会出现重复卡片。
- 教程详情可分享打开，Esc 在浮层模式之外不必关闭整页。
- 管理端下架后，已开着的详情再请求显示不可用。
- 断网时列表出现重试，而不是空白成功页。

## 完成定义

生产构建不读取 `src/data`。`Card` 与 `Gallery` 的调用点只接收 `view.js` 的对象。排序按钮与接口的 `effective_sort` 一致。
