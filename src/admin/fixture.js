import { ApiError } from '../api/http.js'

const LEVEL = { beginner: '入门', advanced: '进阶', unknown: '未知' }
const PRICING = { free: '免费', paid: '付费', freemium: '免费 + 付费', unknown: '未知' }

export function createAdminFixture() {
  const resources = new Map()
  const revisions = new Map()
  const tags = [
    { id: 'tag-chat', name: '对话', slug: 'chat', dimension: 'capability', status: 'active' },
    { id: 'tag-assistant', name: '助手', slug: 'assistant', dimension: 'category', status: 'active' },
  ]
  const featured = []
  const published = []
  let admin = null
  let seq = 1

  function nextId() {
    seq += 1
    return `00000000-0000-4000-8000-${String(seq).padStart(12, '0')}`
  }

  function clone(value) {
    return JSON.parse(JSON.stringify(value))
  }

  function requireAdmin() {
    if (!admin) throw new ApiError({ status: 401, code: 'unauthenticated', message: '未登录' })
  }

  function notFound(message = '不存在') {
    return new ApiError({ status: 404, code: 'not_found', message })
  }

  function conflict() {
    return new ApiError({ status: 409, code: 'edit_conflict', message: '内容已被他人更新' })
  }

  function payloadFrom(body) {
    return {
      title: body.title || '',
      aliases: body.aliases || [],
      summary: body.summary || '',
      body_markdown: body.body_markdown || '',
      cover_urls: body.cover_urls || [],
      primary_category_id: body.primary_category_id || null,
      tag_ids: body.tag_ids || [],
      quality_score: body.quality_score || 0,
      recommendation_reason: body.recommendation_reason || '',
      details: body.details || {},
    }
  }

  function rememberPublic(record) {
    const index = published.findIndex((item) => item.id === record.id)
    const next = toPublic(record, record.published)
    if (!next || record.status !== 'published') {
      if (index >= 0) published.splice(index, 1)
      return
    }
    if (index >= 0) published[index] = next
    else published.push(next)
  }

  function toPublic(record, payload) {
    if (!payload) return null
    const resolved = (payload.tag_ids || [])
      .map((id) => tags.find((tag) => tag.id === id))
      .filter((tag) => tag && tag.status === 'active')
      .map(({ id, name, slug, dimension }) => ({ id, name, slug, dimension }))
    return {
      id: record.id,
      kind: record.kind,
      slug: record.slug,
      title: payload.title,
      summary: payload.summary,
      cover_urls: payload.cover_urls || [],
      cover_fallback_count: 3,
      tags: resolved,
      primary_category: null,
      quality_score: payload.quality_score || 0,
      first_published_at: record.first_published_at,
      content_updated_at: record.updated_at,
      aliases: payload.aliases || [],
      body_markdown: payload.body_markdown || null,
      recommendation_reason: payload.recommendation_reason || null,
      details: payload.details || {},
      card: presentCard(record.kind, payload),
    }
  }

  function detail(record) {
    return {
      id: record.id,
      kind: record.kind,
      slug: record.slug,
      status: record.status,
      edit_version: record.edit_version,
      field_locks: record.field_locks.slice(),
      first_published_at: record.first_published_at,
      draft_revision_id: record.draft_revision_id,
      published_revision_id: record.published_revision_id,
      updated_at: record.updated_at,
      has_unpublished_draft: Boolean(record.draft_revision_id && record.draft_revision_id !== record.published_revision_id),
      draft: record.draft ? { revision_id: record.draft_revision_id, ...clone(record.draft) } : null,
      published: record.published ? { revision_id: record.published_revision_id, ...clone(record.published) } : null,
    }
  }

  function pushRevision(record, payload, reason) {
    const list = revisions.get(record.id) || []
    const revisionId = nextId()
    list.push({
      id: revisionId,
      revision_no: list.length + 1,
      created_at: record.updated_at,
      origin: 'manual',
      change_reason: reason || '',
      payload: clone(payload),
    })
    revisions.set(record.id, list)
    return revisionId
  }

  const api = {
    async login(username, password) {
      if (username === 'admin' && password === 'correct-password') {
        admin = { admin_id: 'admin-1', username: 'admin', expires_at: '2026-10-02T00:00:00Z' }
        return { ...admin }
      }
      throw new ApiError({ status: 401, code: 'unauthenticated', message: '用户名或口令不正确' })
    },
    async logout() {
      requireAdmin()
      admin = null
      return null
    },
    async current() {
      requireAdmin()
      return { ...admin }
    },
    seed(input) {
      const id = input.id || nextId()
      const payload = clone(input.draft || input.payload || input.published)
      const publishedPayload = input.published ? clone(input.published) : (input.status === 'published' ? clone(payload) : null)
      const revisionId = nextId()
      const keepDraft = input.status !== 'published' || input.forceDraft
      const record = {
        id,
        kind: input.kind,
        slug: input.slug,
        status: input.status || 'draft',
        edit_version: input.edit_version || 1,
        field_locks: input.field_locks || [],
        first_published_at: input.first_published_at || null,
        draft_revision_id: keepDraft ? revisionId : null,
        published_revision_id: publishedPayload ? revisionId : null,
        updated_at: input.updated_at || '2026-10-01T00:00:00Z',
        draft: keepDraft ? payload : null,
        published: publishedPayload,
      }
      resources.set(id, record)
      revisions.set(id, [{
        id: revisionId,
        revision_no: 1,
        created_at: record.updated_at,
        origin: 'manual',
        change_reason: 'seed',
        payload: clone(payload),
      }])
      rememberPublic(record)
      return detail(record)
    },
    publishedSnapshot() {
      return published.map((item) => clone(item))
    },
    async listResources(params = {}) {
      requireAdmin()
      const items = [...resources.values()].filter((record) => {
        if (params.kind && record.kind !== params.kind) return false
        if (params.status && record.status !== params.status) return false
        const title = (record.draft || record.published)?.title || ''
        if (params.q && !title.toLowerCase().includes(params.q.trim().toLowerCase())) return false
        return true
      }).map((record) => {
        const payload = record.draft || record.published || {}
        return {
          id: record.id,
          kind: record.kind,
          slug: record.slug,
          title: payload.title || '',
          status: record.status,
          edit_version: record.edit_version,
          has_unpublished_draft: Boolean(record.draft_revision_id && record.draft_revision_id !== record.published_revision_id),
          updated_at: record.updated_at,
        }
      })
      return { items }
    },
    async getResource(id) {
      requireAdmin()
      const record = resources.get(id)
      if (!record) throw notFound()
      return detail(record)
    },
    async createResource(body) {
      requireAdmin()
      const id = nextId()
      const now = new Date().toISOString()
      const record = {
        id,
        kind: body.kind,
        slug: body.slug,
        status: 'draft',
        edit_version: 1,
        field_locks: [],
        first_published_at: null,
        draft_revision_id: null,
        published_revision_id: null,
        updated_at: now,
        draft: null,
        published: null,
      }
      const revisionId = pushRevision(record, payloadFrom(body), body.change_reason)
      record.draft_revision_id = revisionId
      record.draft = payloadFrom(body)
      resources.set(id, record)
      return { id, edit_version: 1, revision_id: revisionId, slug: record.slug, status: 'draft', first_published_at: null, field_locks: [] }
    },
    async saveResource(id, body) {
      requireAdmin()
      const record = resources.get(id)
      if (!record) throw notFound()
      if (body.edit_version !== record.edit_version) throw conflict()
      const payload = payloadFrom(body)
      if (!record.first_published_at && body.slug) record.slug = body.slug
      record.updated_at = new Date().toISOString()
      const revisionId = pushRevision(record, payload, body.change_reason)
      record.draft = payload
      record.draft_revision_id = revisionId
      record.edit_version += 1
      const unlocked = new Set(body.unlock_fields || [])
      record.field_locks = record.field_locks.filter((path) => !unlocked.has(path))
      return {
        id,
        edit_version: record.edit_version,
        revision_id: revisionId,
        slug: record.slug,
        status: record.status,
        first_published_at: record.first_published_at,
        field_locks: record.field_locks.slice(),
      }
    },
    async publish(id, body) {
      requireAdmin()
      const record = resources.get(id)
      if (!record) throw notFound()
      if (body.edit_version !== record.edit_version || body.revision_id !== record.draft_revision_id) throw conflict()
      record.published = clone(record.draft)
      record.published_revision_id = record.draft_revision_id
      record.draft = null
      record.draft_revision_id = null
      record.status = 'published'
      record.edit_version += 1
      if (!record.first_published_at) record.first_published_at = new Date().toISOString()
      record.updated_at = record.first_published_at
      rememberPublic(record)
      return {
        id,
        edit_version: record.edit_version,
        revision_id: record.published_revision_id,
        slug: record.slug,
        status: record.status,
        first_published_at: record.first_published_at,
      }
    },
    async setVisibility(id, body) {
      requireAdmin()
      const record = resources.get(id)
      if (!record) throw notFound()
      if (body.edit_version !== record.edit_version) throw conflict()
      record.status = body.status
      record.edit_version += 1
      record.updated_at = new Date().toISOString()
      rememberPublic(record)
      return { id, edit_version: record.edit_version, status: record.status }
    },
    async preview(id) {
      requireAdmin()
      const record = resources.get(id)
      if (!record) throw notFound()
      const payload = record.draft || record.published
      if (!payload) throw notFound('没有可预览的内容')
      return toPublic(record, payload)
    },
    async listRevisions(id) {
      requireAdmin()
      if (!resources.has(id)) throw notFound()
      return { revisions: clone(revisions.get(id) || []) }
    },
    async listTags() {
      requireAdmin()
      return { tags: tags.filter((tag) => tag.status !== 'merged').map((tag) => clone(tag)) }
    },
    async createTag(body) {
      requireAdmin()
      const tag = { id: nextId(), name: body.name, slug: body.slug, dimension: body.dimension, status: 'active' }
      tags.push(tag)
      return clone(tag)
    },
    async mergeTag(id, body) {
      requireAdmin()
      const source = tags.find((tag) => tag.id === id)
      const target = tags.find((tag) => tag.id === body.target_id)
      if (!source || !target) throw notFound('标签不存在')
      source.status = 'merged'
      source.merged_into_id = target.id
      return { id: source.id, merged_into_id: target.id }
    },
    async listFeatured() {
      requireAdmin()
      return {
        items: featured.filter((slot) => slot.enabled !== false).map((slot) => {
          const record = resources.get(slot.resource_id)
          return { ...slot, resource_slug: record?.slug || slot.resource_slug || '' }
        }),
      }
    },
    async createFeatured(body) {
      requireAdmin()
      const record = [...resources.values()].find((item) => item.id === body.resource_id || item.slug === body.resource_slug)
      if (!record) throw new ApiError({ status: 400, code: 'invalid_argument', message: '没有找到这个 slug', fieldErrors: [{ field: 'resource_slug', code: 'missing' }] })
      const slot = {
        id: nextId(),
        kind: body.kind,
        placement: body.placement || 'hero',
        position: Number(body.position),
        resource_id: record.id,
        resource_slug: record.slug,
        starts_at: body.starts_at,
        ends_at: body.ends_at || null,
        enabled: true,
      }
      const hit = featured.some((item) => item.enabled !== false && overlaps(item, slot))
      if (hit) {
        throw new ApiError({
          status: 409,
          code: 'invalid_argument',
          message: 'overlap',
          fieldErrors: [{ field: 'starts_at', code: 'overlap' }],
        })
      }
      featured.push(slot)
      return clone(slot)
    },
    async deleteFeatured(id) {
      requireAdmin()
      const slot = featured.find((item) => item.id === id)
      if (!slot) throw notFound()
      slot.enabled = false
      return null
    },
  }

  return api
}

function presentCard(kind, payload) {
  const details = payload.details || {}
  if (kind === 'tutorial') {
    const steps = (details.steps || []).filter(Boolean)
    const subtitle = steps.length ? `${details.minutes || 0} 分钟 · ${steps.length} 步` : `${details.minutes || 0} 分钟`
    return { subtitle, meta: LEVEL[details.level] || '未知', href: null, cta: '查看教程' }
  }
  if (kind === 'repo') {
    const owner = String(details.full_name || '').split('/')[0] || ''
    return {
      subtitle: owner,
      meta: details.language || '未知语言',
      href: details.full_name ? `https://github.com/${details.full_name}` : null,
      cta: 'GitHub',
    }
  }
  let host = ''
  try { host = new URL(details.website_url).hostname.replace(/^www\./, '') } catch { host = '' }
  return { subtitle: host, meta: PRICING[details.pricing] || '未知', href: details.website_url || null, cta: '访问' }
}

function overlaps(a, b) {
  if (a.kind !== b.kind || a.placement !== b.placement || a.position !== b.position) return false
  const start = (value) => Date.parse(value)
  const end = (value) => (value ? Date.parse(value) : Infinity)
  return start(a.starts_at) < end(b.ends_at) && start(b.starts_at) < end(a.ends_at)
}
