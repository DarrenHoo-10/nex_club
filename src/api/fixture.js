import { ApiError } from './http.js'

const TAGS = {
  chat: { id: 'tag-chat', name: '对话', slug: 'chat', dimension: 'capability' },
  local: { id: 'tag-local', name: '本地部署', slug: 'local', dimension: 'capability' },
  agent: { id: 'tag-agent', name: 'Agent', slug: 'agent', dimension: 'capability' },
  creation: { id: 'tag-creation', name: '创作', slug: 'creation', dimension: 'capability' },
  productivity: { id: 'tag-productivity', name: '效率', slug: 'productivity', dimension: 'capability' },
  beginner: { id: 'tag-beginner', name: '入门', slug: 'beginner', dimension: 'capability' },
}

const RESOURCES = [
  {
    id: '6f1c0000-0000-4000-8000-000000000001',
    kind: 'tool',
    slug: 'claude',
    title: 'Claude',
    summary: 'Anthropic 出品的 AI 助手',
    cover_urls: [],
    cover_fallback_count: 3,
    tags: [TAGS.chat],
    primary_category: null,
    quality_score: 80,
    first_published_at: '2026-09-12T12:00:00Z',
    content_updated_at: '2026-09-12T12:00:00Z',
    aliases: [],
    body_markdown: null,
    recommendation_reason: '长文和代码都稳。',
    details: { website_url: 'https://claude.ai', pricing: 'freemium', platforms: ['web'], deployment: ['hosted'] },
    card: { subtitle: 'claude.ai', meta: '免费 + 付费', href: 'https://claude.ai', cta: '访问' },
    featured: true,
  },
  {
    id: '6f1c0000-0000-4000-8000-000000000002',
    kind: 'tutorial',
    slug: 'claude-subscribe',
    title: '如何订阅 Claude 会员',
    summary: '从注册账号到开通 Pro 套餐的完整流程。',
    cover_urls: [],
    cover_fallback_count: 3,
    tags: [TAGS.chat],
    primary_category: null,
    quality_score: 60,
    first_published_at: '2026-09-16T12:00:00Z',
    content_updated_at: '2026-09-16T12:00:00Z',
    aliases: [],
    body_markdown: '通过官方页面完成订阅。',
    recommendation_reason: null,
    details: {
      level: 'beginner',
      minutes: 5,
      steps: ['打开 claude.ai 注册。', '选择 Pro 套餐并付款。'],
      author: '',
      source_url: 'https://claude.ai',
      notes: '请只通过官方页面付款。',
    },
    card: { subtitle: '5 分钟 · 2 步', meta: '入门', href: null, cta: '查看教程' },
    featured: true,
  },
  {
    id: '6f1c0000-0000-4000-8000-000000000003',
    kind: 'repo',
    slug: 'ollama',
    title: 'ollama',
    summary: '一条命令在本地运行开源大模型。',
    cover_urls: [],
    cover_fallback_count: 3,
    tags: [TAGS.local],
    primary_category: null,
    quality_score: 70,
    first_published_at: '2026-09-10T12:00:00Z',
    content_updated_at: '2026-09-10T12:00:00Z',
    aliases: [],
    body_markdown: null,
    recommendation_reason: '本地试模型很快。',
    details: {
      github_repository_id: '',
      full_name: 'ollama/ollama',
      language: 'Go',
      license: 'MIT',
      archived: false,
      last_activity_at: '2026-09-20T00:00:00Z',
    },
    card: { subtitle: 'ollama', meta: 'Go', href: 'https://github.com/ollama/ollama', cta: 'GitHub' },
    featured: true,
  },
  {
    id: '6f1c0000-0000-4000-8000-000000000004',
    kind: 'tutorial',
    slug: 'chatcut-video-agent',
    title: '超简单 ChatCut 使用，Codex大白话剪辑出爆款视频',
    summary: '一个人做出海，工具就是同事。今天这位新同事叫 ChatCut：不发工资、上手快，只有一个要求——你得先开口，把想要的讲清楚。用大白话让 Codex 剪辑出爆款视频。',
    cover_urls: ['/covers/chatcut-1.jpg'],
    cover_fallback_count: 3,
    tags: [TAGS.agent, TAGS.creation, TAGS.productivity, TAGS.beginner],
    primary_category: null,
    quality_score: 95,
    first_published_at: '2026-10-04T13:11:43Z',
    content_updated_at: '2026-10-04T13:11:43Z',
    aliases: ['chatcut'],
    body_markdown: `> 一个人做出海，工具就是同事。  
> 今天这位新同事叫 **ChatCut**。不发工资，上手快，只有一个要求——你得先开口，把想要的讲清楚。  
> ChatCut 是一个用说话来剪的 AI 剪辑工具。把想要什么描述出来，它把片子剪好。  
> **它和剪映不是一回事。剪映是自己动手剪，它替你动手。**

---

### 📑 目录

- **一、ChatCut 是什么** —— 一句话交代需求，它自己剪
- **二、三个入口，装哪个** —— 官方推荐桌面端，Codex 怎么接上
- **三、界面认一遍** —— 素材、AI 面板、时间线
- **四、第一刀：素材进来，说一句话** —— 从一堆素材到第一版成片
- **五、字幕** —— 改字就是改视频
- **六、导出** —— 顺带说说变成剪映草稿
- **七、还能让它干什么** —— 八条能直接抄的指令
- **八、算一下钱** —— 免费档和 Pro
- **九、它和剪映怎么配合** —— 不是二选一
- **十、优势 & 常见坑速查表**

---

### 一、ChatCut 是什么

先说结论：**剪映给的是工具，ChatCut 给的是结果。**

剪映里，你拖素材、切片段、调音量，一帧一帧自己摆。软件负责顺手，动手的是人。  
ChatCut 走的是另一条路。把要求说出来——挑高光、排顺序、配画面、加字幕，它先给你一版。  
一个是“你开车”，一个是“你说去哪”。

> **💡 它最容易被忽略的一点：它能导出成可编辑的剪映草稿。**  
> 这句话的分量在这儿：AI 剪出来的东西不会全合心意。导出成剪映草稿，就能回到熟悉的时间线上接着改——而不是被锁在一个还没学会的工具里重来。  
> 就冲这一条，两个工具不用二选一！

---

### 二、三个入口，装哪个

官网地址：[chatcut.io](https://chatcut.io/)  
注册推荐大家用个邮箱或者谷歌账号直接登录就好。

官方把入口分成三个：**ChatCut 桌面端**、**ChatCut 网页端**、**ChatCut Agent Plugin**（插件底下包含 Claude Code 和 Codex）。

| 入口 | 怎么进 | 适合场景 |
| :--- | :--- | :--- |
| **ChatCut 桌面端** | 网页端登录后，点头像「桌面应用」，选平台下载 | **官方推荐**，适合大多数剪辑的活；直接读取本地文件和文件夹，本地跑渲染，不消耗云端分钟数 |
| **ChatCut 网页端** | \`app.chatcut.io\` | 想立刻开始、云端项目、协作；新手先试手，不用装，打开就能用 |
| **Agent Plugin** | 在 Claude Code 或 Codex 桌面应用里新开一个对话安装 | 对话留在那边，连接任意智能体（ChatGPT / Codex / Claude Code / WorkBuddy 等）直接操控工程 |

> 💻 **支持系统**：桌面端目前支持 macOS (Apple Silicon)、macOS (Intel)、Windows，暂无 Linux 版。

**两个版本真正影响日常的差别：**

| 对比项 | 网页端 | 桌面端 |
| :--- | :--- | :--- |
| **素材** | 要先上传到云端 | 直接读本地文件和文件夹 |
| **导出在跑哪里** | 云端服务器 | 本机硬件渲染（性能足够还支持 4K 和 HDR） |
| **导出的量** | 免费档：所有项目**累计 60 分钟** | **本机不计入那 60 分钟**（完全免费不烧额度） |

*注意：网页端免费档那 60 分钟是「所有项目加起来」的总量，不是每月重来一次。走桌面端导出不烧这个额度——素材多、片子长，这一条就够决定装不装桌面端！*

---

### 三、Codex / Claude Code 怎么接上

这一条不用手写复杂的配置，官方给的是一句提示词，剩下交给智能体自己干：

1. 先在 ChatCut 网页端登录；
2. 点右上角头像，鼠标停在「Agent Plugin」上；
3. 在「Claude Code」或「ChatGPT/Codex」旁边点「复制」；
4. 把复制到的那句提示词，粘到对应的桌面应用里发出去：
\`\`\`text
Call get_active_project through the chatcut_desktop MCP server to connect to...
\`\`\`
5. 等它弹出 ChatCut 授权页，在上面登录并允许访问即可自动连通！

---

### 四、第一刀：素材进来，说一句话

从一堆素材到第一版成片，只需在对话框输入自然语言口播指令：

> **实战口播指令：**  
> “帮我把这个 5 分钟的口播录屏粗剪一下：把超过 1 秒的无声停顿和重复的口误都切掉，整体节奏紧凑一点，适合发 TikTok。”

---

### 五、字幕：改字就是改视频

- **文字即剪辑**：AI 自动高精度语音转文字对齐时间轴；直接修改字幕文本，视频画面自动对应剪切；
- **动态爆款包装**：一句话生成大字号、黄黑高对比度配色的跳动强调字幕。

---

### 六、导出：顺带说说变成剪映草稿

- 点击导出不仅能直接输出 MP4 高清成片；
- **王牌功能**：直接选择「导出为剪映草稿」！导出的工程可在剪映电脑端无缝打开，完整保留音视频切片、时间轨道与字幕，让你做最终的精细化包装。

---

### 七、还能让它干什么：八条能直接抄的指令

1. **去气口粗剪**：“切掉所有静音超过 0.8 秒的段落，口语磕绊的重说部分只留最后一次。”
2. **高潮提炼**：“提取整段访谈中最精彩的 30 秒金句，按短视频节奏做成片头引子。”
3. **爆款字幕**：“生成动态英文双语字幕，关键词用黄色突出显示并加微缩放动效。”
4. **BGM 智能混音**：“配一段轻快节奏的背景音乐，人声说话时 BGM 音量自动降到 25%。”
5. **画中画与特写**：“在讲到关键数据时，把画面推近放大 20% 特写，并打出强调字效。”
6. **竖屏重构**：“把横屏视频自动识别主体居中裁剪为 9:16 竖屏，顶部加标题框。”
7. **多片段转场**：“在不同场景切镜处添加轻微的快速推镜或淡入淡出，不要用花哨转场。”
8. **一键工程导出**：“将当前全部剪辑成果打包导出为 CapCut 剪映电脑端草稿。”

---

### 八、算一下钱：免费档和 Pro

- **免费档**：桌面端本地跑完全免费，导出不限次数不耗云端额度；网页端云端累计 60 分钟；
- **Pro 档**：适合团队多端云同步、重度云渲染与高级商用音视频资产库。

---

### 九、它和剪映怎么配合：不是二选一

- **工作流黄金组合**：ChatCut 负责搞定前 80% 的体力劳动（粗剪、切废话、文本同步、音画粗配）；剪映负责后 20% 的审美把控（精修色调、个性化贴纸、品牌水印）。效率直接翻倍！

---

### 十、优势 & 常见坑速查表

| 类别 | 要点 | 避坑提醒 |
| :--- | :--- | :--- |
| **额度陷阱** | 网页端 60 分钟是终身总额度，非每月重置 | **避坑**：素材量大或长视频务必下载桌面端在本地导出，不耗额度 |
| **指令细节** | 越清晰具体，AI 初剪越准 | **避坑**：不要只说“剪好看点”，要说明目标平台（TikTok）、节奏快慢与具体保留要求 |
| **工程协同** | 剪映草稿导出是王牌功能 | **避坑**：导出剪映草稿后，不要移动或重命名本地原始视频文件的路径 |
| **长视频拆解** | Agent 上下文长文本处理 | **避坑**：超过 30 分钟的长素材，建议按章节拆分成 3~5 段分别让 Agent 处理后拼接 |`,
    recommendation_reason: '大白话就能驱动视频剪辑，一人出海做视频内容的神器。',
    details: {
      level: 'beginner',
      minutes: 8,
      steps: [
        '安装 ChatCut 插件与 MCP 端点：在 Codex 或 Claude Code 配置 ChatCut 官方 MCP 服务，并完成账户授权连接。',
        '准备原始音视频素材：将要剪辑的录屏、口播或演示视频放入项目目录，让 Agent 获取素材文件上下文。',
        '大白话下达剪辑指令：用日常口语描述需求（如“去除开头无声和口误、保留干货，按快节奏短视频剪出前 30 秒”）。',
        'AI 智能生成字幕与动态包装：让 Agent 自动识别语音生成时间轴对齐的精美字幕，并根据高潮点插入动态贴字与转场。',
        '预览工程与一键成片导出：在 ChatCut 预览窗口检查剪辑轨道，微调后通过指令由 Agent 自动渲染并导出高清视频。',
      ],
      author: '想风 (@xaiwind)',
      source_url: 'https://x.com/xaiwind/status/2106734008436707477',
      notes: '大白话指令越具体，AI 剪辑越符合预期；建议明确视频受众与平台风格（如 TikTok/YouTube Shorts 快节奏）。大工程建议按镜头或章节分段让 AI 加工，以保持最高剪辑精度。',
    },
    card: { subtitle: '想风 (@xaiwind) · 8 分钟', meta: '入门', href: 'https://x.com/xaiwind/status/2106734008436707477', cta: '查看教程' },
    featured: true,
  },
]

function notFound() {
  return new ApiError({ status: 404, code: 'not_found', message: 'not found', requestId: 'fixture' })
}

function matches(item, { kind, q, tag }) {
  if (kind && item.kind !== kind) return false
  const tags = Array.isArray(tag) ? tag : tag ? [tag] : []
  if (tags.length && !tags.every((slug) => item.tags.some((entry) => entry.slug === slug))) return false
  const text = q.trim().toLowerCase()
  if (!text) return true
  const haystack = [
    item.title,
    item.summary,
    item.body_markdown,
    ...(item.details?.steps || []),
    ...item.tags.map((entry) => entry.name),
  ].filter(Boolean).join(' ').toLowerCase()
  return haystack.includes(text)
}

function effectiveSort(sort, q) {
  if (sort === 'relevance' && !q.trim()) return 'recommended'
  if (sort) return sort
  return q.trim() ? 'relevance' : 'recommended'
}

function page(items, sort) {
  return {
    items,
    next_cursor: null,
    has_more: false,
    total: null,
    effective_sort: sort,
    ranking_version: null,
    ranking_computed_at: null,
    applied_query: { kind: items[0]?.kind || '', q: '', tags: [], sort },
  }
}

export function createFixtureClient() {
  return {
    async listResources(params = {}) {
      const q = params.q || ''
      const sort = effectiveSort(params.sort, q)
      let items = RESOURCES.filter((item) => matches(item, { kind: params.kind, q, tag: params.tag }))
      if (sort === 'latest') {
        items = items.slice().sort((a, b) => Date.parse(b.first_published_at) - Date.parse(a.first_published_at))
      }
      if (params.cursor) return page([], sort)
      return page(items.map(publicResource), sort)
    },
    async getBySlug(slug) {
      const found = RESOURCES.find((item) => item.slug === slug)
      if (!found) throw notFound()
      return publicResource(found)
    },
    async listTags(params = {}) {
      const items = RESOURCES.filter((item) => matches(item, { kind: params.kind, q: params.q || '', tag: '' }))
      const map = new Map()
      items.forEach((item) => {
        item.tags.forEach((tag) => {
          const prev = map.get(tag.slug) || { ...tag, count: 0 }
          prev.count += 1
          map.set(tag.slug, prev)
        })
      })
      return { tags: [...map.values()] }
    },
    async listFeatured(kind) {
      const items = RESOURCES.filter((item) => item.kind === kind && item.featured).map((item, index) => ({
        ...publicResource(item),
        position: index + 1,
      }))
      return { items }
    },
    async postEvents(events) {
      return { accepted: events?.length || 0, duplicate: 0 }
    },
  }
}

function publicResource(item) {
  const resource = { ...item }
  delete resource.featured
  return resource
}
