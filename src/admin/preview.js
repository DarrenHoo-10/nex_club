import { safeHttpUrl } from '../api/view.js'
import { toWriteBody } from './editorState.js'
import { LEVELS, PRICING } from './labels.js'

// Unsaved previews remain in this browser; saving and publishing are separate actions.
export function previewFromForm(form, tags, id) {
  const body = toWriteBody(form, { includeVersion: false })
  return {
    id: id || `preview-${form.kind}`,
    kind: form.kind,
    slug: form.slug,
    title: body.title,
    summary: body.summary,
    body_markdown: body.body_markdown,
    recommendation_reason: body.recommendation_reason,
    cover_urls: body.cover_urls.map(safeHttpUrl).filter(Boolean),
    cover_fallback_count: 3,
    tags: tags.filter((tag) => body.tag_ids.includes(tag.id)),
    details: body.details,
    card: previewCard(form.kind, body.details),
  }
}

function previewCard(kind, details) {
  if (kind === 'tutorial') {
    const count = details.steps.length
    return {
      subtitle: `${details.minutes} 分钟${count ? ` · ${count} 步` : ''}`,
      meta: label(LEVELS, details.level),
      href: null,
    }
  }
  if (kind === 'repo') {
    const fullName = details.full_name.trim()
    return {
      subtitle: fullName.split('/')[0],
      meta: details.language.trim() || '未知语言',
      href: /^[^/\s?#]+\/[^/\s?#]+$/.test(fullName) ? `https://github.com/${fullName}` : null,
    }
  }
  const href = safeHttpUrl(details.website_url)
  return {
    subtitle: href ? new URL(href).hostname.replace(/^www\./, '') : '',
    meta: label(PRICING, details.pricing),
    href: href || null,
  }
}

function label(options, value) {
  return options.find(([code]) => code === value)?.[1] || '未知'
}
