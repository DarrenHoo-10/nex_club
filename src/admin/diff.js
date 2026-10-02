const FIELDS = ['title', 'aliases', 'summary', 'body_markdown', 'cover_urls', 'primary_category_id', 'tag_ids', 'quality_score', 'recommendation_reason']

export function diffPayload(left = {}, right = {}) {
  const changes = []
  FIELDS.forEach((field) => {
    if (JSON.stringify(left[field] ?? null) !== JSON.stringify(right[field] ?? null)) {
      changes.push({ field, from: left[field] ?? null, to: right[field] ?? null })
    }
  })
  const a = left.details || {}
  const b = right.details || {}
  const keys = [...new Set([...Object.keys(a), ...Object.keys(b)])].sort()
  keys.forEach((key) => {
    if (JSON.stringify(a[key] ?? null) !== JSON.stringify(b[key] ?? null)) {
      changes.push({ field: `details.${key}`, from: a[key] ?? null, to: b[key] ?? null })
    }
  })
  return changes
}

export function formatValue(value) {
  if (value == null || value === '') return '空'
  if (typeof value === 'string') return value
  return JSON.stringify(value)
}
