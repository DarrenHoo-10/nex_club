const LEVEL_LABEL = { unknown: '未知', beginner: '入门', advanced: '进阶' }

export function toGalleryKind(kind) {
  if (kind === 'tool') return 'site'
  if (kind === 'tutorial') return 'doc'
  return 'repo'
}

export function safeHttpUrl(value) {
  if (typeof value !== 'string') return ''
  const trimmed = value.trim()
  if (!/^https?:\/\//i.test(trimmed)) return ''
  try {
    const url = new URL(trimmed)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return ''
    if (url.username || url.password) return ''
    return url.href
  } catch {
    return ''
  }
}

export function toCardModel(resource) {
  const tags = Array.isArray(resource.tags) ? resource.tags : []
  const href = resource.card?.href || null
  const steps = Array.isArray(resource.details?.steps) ? resource.details.steps : []
  return {
    id: resource.id,
    slug: resource.slug,
    kind: resource.kind,
    title: resource.title,
    desc: resource.summary || '',
    tags: tags.map((tag) => tag.name),
    tagSlugs: tags.map((tag) => tag.slug),
    covers: Array.isArray(resource.cover_urls) ? resource.cover_urls : [],
    coverCount: resource.cover_fallback_count || 3,
    subtitle: resource.card?.subtitle || '',
    meta: resource.card?.meta || '',
    href,
    cta: resource.card?.cta || '',
    name: resource.title,
    url: safeHttpUrl(href) || 'https://example.com',
    repo: coverRepo(resource),
    lang: resource.details?.language || '',
    steps,
  }
}

export function toReaderModel(resource) {
  const details = resource.details || {}
  const steps = Array.isArray(details.steps) ? details.steps.filter((step) => typeof step === 'string') : []
  const notes = typeof details.notes === 'string' && details.notes.trim() ? details.notes : null
  const body = typeof resource.body_markdown === 'string' && resource.body_markdown.trim() ? resource.body_markdown : null
  const minutes = Number.isFinite(details.minutes) ? details.minutes : 0
  return {
    title: resource.title || '',
    levelLabel: resource.card?.meta || LEVEL_LABEL[details.level] || '未知',
    minutes,
    bodyMarkdown: body,
    steps,
    notes,
  }
}

function coverRepo(resource) {
  const full = resource.details?.full_name
  if (typeof full === 'string' && full.includes('/')) return full
  const href = resource.card?.href || ''
  try {
    const parts = new URL(href).pathname.split('/').filter(Boolean)
    if (parts.length >= 2) return `${parts[0]}/${parts[1]}`
  } catch {
    // Generated covers only need a stable owner/name pair.
  }
  const owner = resource.card?.subtitle
  return owner ? `${owner}/repo` : 'owner/repo'
}
