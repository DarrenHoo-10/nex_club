export const SLUG_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

export function slugValid(slug) {
  return slug.length >= 1 && slug.length <= 80 && SLUG_RE.test(slug)
}

export function defaultDetails(kind) {
  if (kind === 'tutorial') return { level: 'unknown', minutes: 0, steps: [], author: '', source_url: '', notes: '' }
  if (kind === 'repo') return { github_repository_id: '', full_name: '', language: '', license: '', archived: false, last_activity_at: '' }
  return { website_url: '', pricing: 'unknown', platforms: [], deployment: [] }
}

export function emptyForm(kind) {
  return {
    editVersion: 0,
    revisionId: '',
    kind,
    slug: '',
    title: '',
    aliasesText: '',
    summary: '',
    bodyMarkdown: '',
    coverUrlsText: '',
    primaryCategoryId: '',
    tagIds: [],
    qualityScore: 0,
    recommendationReason: '',
    details: defaultDetails(kind),
    unlockFields: [],
    changeReason: '',
    firstPublishedAt: null,
    fieldLocks: [],
    status: 'draft',
    hasDraft: false,
  }
}

export function formFromResource(resource) {
  const source = resource.draft || resource.published || {}
  return {
    editVersion: resource.edit_version,
    revisionId: source.revision_id || resource.draft_revision_id || resource.published_revision_id || '',
    kind: resource.kind,
    slug: resource.slug || '',
    title: source.title || '',
    aliasesText: lines(source.aliases),
    summary: source.summary || '',
    bodyMarkdown: source.body_markdown || '',
    coverUrlsText: lines(source.cover_urls),
    primaryCategoryId: source.primary_category_id || '',
    tagIds: source.tag_ids || [],
    qualityScore: source.quality_score ?? 0,
    recommendationReason: source.recommendation_reason || '',
    details: normalizeDetails(resource.kind, source.details),
    unlockFields: [],
    changeReason: '',
    firstPublishedAt: resource.first_published_at,
    fieldLocks: resource.field_locks || [],
    status: resource.status,
    hasDraft: Boolean(resource.draft),
  }
}

export function toWriteBody(form, { includeVersion = true } = {}) {
  const body = {
    slug: form.slug.trim(),
    title: form.title,
    aliases: splitLines(form.aliasesText),
    summary: form.summary,
    body_markdown: form.bodyMarkdown,
    cover_urls: splitLines(form.coverUrlsText),
    primary_category_id: form.primaryCategoryId || null,
    tag_ids: form.tagIds,
    quality_score: Number(form.qualityScore) || 0,
    recommendation_reason: form.recommendationReason,
    details: detailsForWrite(form.kind, form.details),
    unlock_fields: form.unlockFields,
    change_reason: form.changeReason,
  }
  if (includeVersion) body.edit_version = form.editVersion
  return body
}

export function contentKey(form) {
  const body = toWriteBody(form, { includeVersion: false })
  return JSON.stringify(body)
}

export function canSave(form) {
  if (!slugValid(form.slug.trim()) || !form.title.trim() || !form.summary.trim()) return false
  if (form.kind === 'tool' && !form.details.website_url.trim()) return false
  if (form.kind === 'repo' && !form.details.full_name.trim()) return false
  if (form.kind === 'tutorial') {
    const steps = (form.details.steps || []).map((step) => step.trim()).filter(Boolean)
    if (!form.bodyMarkdown.trim() && steps.length === 0) return false
  }
  return true
}

export function normalizeDetails(kind, details) {
  const base = defaultDetails(kind)
  const src = details || {}
  if (kind === 'tutorial') {
    return {
      ...base,
      level: src.level || 'unknown',
      minutes: src.minutes ?? 0,
      steps: Array.isArray(src.steps) ? src.steps.map((step) => String(step)) : [],
      author: src.author || '',
      source_url: src.source_url || '',
      notes: src.notes || '',
    }
  }
  if (kind === 'repo') {
    return {
      ...base,
      github_repository_id: src.github_repository_id || '',
      full_name: src.full_name || '',
      language: src.language || '',
      license: src.license || '',
      archived: Boolean(src.archived),
      last_activity_at: toUtcInput(src.last_activity_at),
    }
  }
  return {
    ...base,
    website_url: src.website_url || '',
    pricing: src.pricing || 'unknown',
    platforms: src.platforms || [],
    deployment: src.deployment || [],
  }
}

export function toUtcInput(value) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part) => String(part).padStart(2, '0')
  return `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())}T${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`
}

export function fromUtcInput(value) {
  if (!value) return null
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return `${value}:00Z`
  return value
}

function detailsForWrite(kind, details) {
  if (kind === 'tutorial') {
    return {
      level: details.level || 'unknown',
      minutes: Number(details.minutes) || 0,
      steps: (details.steps || []).map((step) => step.trim()).filter(Boolean),
      author: details.author || '',
      source_url: details.source_url?.trim() ? details.source_url.trim() : null,
      notes: details.notes || '',
    }
  }
  if (kind === 'repo') {
    return {
      github_repository_id: details.github_repository_id || '',
      full_name: details.full_name || '',
      language: details.language || '',
      license: details.license || '',
      archived: Boolean(details.archived),
      last_activity_at: fromUtcInput(details.last_activity_at),
    }
  }
  return {
    website_url: details.website_url?.trim() || '',
    pricing: details.pricing || 'unknown',
    platforms: details.platforms || [],
    deployment: details.deployment || [],
  }
}

function splitLines(text) {
  return String(text || '').split('\n').map((line) => line.trim()).filter(Boolean)
}

function lines(values) {
  return Array.isArray(values) ? values.join('\n') : ''
}
