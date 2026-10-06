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

---

### 为什么选择 ChatCut + Codex？

传统视频剪辑最大的痛点在于繁琐的手工操作：拉时间线、切气口、对字幕、调关键帧。对于一人出海或独立创作者而言，内容制作往往消耗了过半的精力。

ChatCut 将视频工程全面接入了 **MCP（Model Context Protocol）** 协议，让 Codex、Claude Code 或 Cursor 等智能体直接化身为你的**专业剪辑助理**：
- **零学习成本**：无需熟记专业剪辑快捷键，打字即剪辑。
- **全流程自动化**：自动剔除无声片段与语气词、按短视频爆款节奏卡点。
- **时间轴精准同步**：AI 自动转录并对齐双语字幕，生成动态花字与音效。

---

### 实战步骤与提示词示例

#### 1. 配置 ChatCut Agent 插件
通过官方 MCP 端点快速将 ChatCut 连接到你的 Codex：
\`\`\`text
https://api.chatcut.io/api/external-mcp/mcp
\`\`\`
在支持 MCP 的终端或配置中完成授权登录：
\`\`\`bash
codex mcp login chatcut
\`\`\`

#### 2. 大白话提示词（Prompt）剪辑实操
素材导入后，直接在对话框输入日常口语指令即可驱动剪辑：

**场景 A：口播粗剪与自动剔除废话**
> “帮我把这个 5 分钟的口播录屏粗剪一下：把超过 1 秒的无声停顿和重复的口误都切掉，整体节奏紧凑一点，适合发 TikTok。”

**场景 B：自动字幕与爆款花字**
> “识别视频里讲的内容，生成大字号、黄黑配色的动态英文爆款字幕，重点强调词加上微缩放动画效果。”

**场景 C：BGM 混音与音量避让**
> “帮我配一段节奏欢快的背景音乐，说话时背景音自动降低 60%，没有说话时音乐音量恢复正常。”

---

### 核心亮点与出海建议

- **高效批量产出**：一人团队也能实现每天产出多条高质量社媒营销视频。
- **本地+云端协作**：本地编写脚本与提示词，云端渲染高质量成片，不占用本地显卡资源。
- **善用上下文反馈**：生成后若对某几个转场不满意，直接指出“第 15 秒转场换成快速推镜”，Agent 就会在时间线上精准定位修改。`,
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
