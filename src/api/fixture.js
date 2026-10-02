import { ApiError } from './http.js'

const TAGS = {
  chat: { id: 'tag-chat', name: '对话', slug: 'chat', dimension: 'capability' },
  local: { id: 'tag-local', name: '本地部署', slug: 'local', dimension: 'capability' },
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
