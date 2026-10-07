import { Children, Component, isValidElement } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { safeHttpUrl } from '../api/view.js'

const NOTICE = '正文格式无法完整解析，以下为原文。'

// IDs come from parsed headings, so fenced code never becomes a TOC entry.
// Count repeated labels independently; Chinese headings remain readable in URLs.
export function headingAnchors() {
  return (tree) => {
    const used = new Set()
    const text = (node) => node.value || node.alt || (node.children || []).map(text).join('')
    const visit = (node) => {
      if (node.type === 'heading') {
        const label = text(node).normalize('NFKC').toLowerCase().trim().replace(/[^\p{L}\p{N}\s-]/gu, '').replace(/\s+/g, '-') || 'heading'
        const base = `section-${label}`
        let id = base
        let suffix = 2
        while (used.has(id)) id = `${base}-${suffix++}`
        used.add(id)
        node.data = { ...node.data, hProperties: { ...node.data?.hProperties, id, tabIndex: -1 } }
      }
      node.children?.forEach(visit)
    }
    visit(tree)
  }
}

function markdownUrl(value) {
  if (/^#[^\s]*$/.test(value) || /^\/(?!\/)/.test(value)) return value
  return safeHttpUrl(value)
}

function mediaSrc(src) {
  const safe = safeHttpUrl(src)
  if (safe) return safe
  if (/^\/(?!\/)/.test(src || '')) return src
  return ''
}

// A markdown image whose address is a video file plays inline.
// The image title, when it is an http(s) URL, is the poster frame.
export function isDirectVideo(url) {
  try {
    return /\.(mp4|webm|m4v|mov)$/i.test(new URL(url, 'https://local.invalid').pathname)
  } catch {
    return false
  }
}

function image({ src, alt, title }) {
  const safe = mediaSrc(src)
  if (!safe) return null
  if (isDirectVideo(safe)) {
    const poster = safeHttpUrl(title)
    return (
      <video
        src={safe}
        poster={poster || undefined}
        controls
        playsInline
        preload="metadata"
        aria-label={alt || undefined}
      />
    )
  }
  return <img src={safe} alt={alt || ''} loading="lazy" />
}

function paragraph({ children }) {
  const nodes = Children.toArray(children).filter((child) => typeof child !== 'string' || child.trim() !== '')
  const only = nodes.length === 1 && isValidElement(nodes[0]) ? nodes[0] : null
  if (only?.type === image && isDirectVideo(mediaSrc(only.props.src))) {
    return <div className="reader-video">{only}</div>
  }
  return <p>{children}</p>
}

export default function MarkdownBody({ source }) {
  return (
    <MarkdownErrorBoundary source={source}>
      <div className="reader-body">
        <ReactMarkdown
          remarkPlugins={[remarkGfm, headingAnchors]}
          urlTransform={markdownUrl}
          components={{
            a({ href, children }) {
              if (href?.startsWith('#')) return <a href={href}>{children}</a>
              const safe = safeHttpUrl(href)
              if (!safe) return <span>{children}</span>
              return <a href={safe} target="_blank" rel="nofollow noopener noreferrer">{children}</a>
            },
            p: paragraph,
            img: image,
            table({ children }) {
              return <div className="reader-table" role="region" aria-label="表格（可横向滚动）" tabIndex={0}><table>{children}</table></div>
            },
          }}
        >
          {source}
        </ReactMarkdown>
      </div>
    </MarkdownErrorBoundary>
  )
}

class MarkdownErrorBoundary extends Component {
  constructor(props) {
    super(props)
    this.state = { failed: false, source: props.source }
  }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  static getDerivedStateFromProps(props, state) {
    if (props.source !== state.source) return { failed: false, source: props.source }
    return null
  }

  render() {
    if (this.state.failed) {
      return (
        <div className="reader-fallback">
          <p className="reader-note">{NOTICE}</p>
          <p className="reader-plain">{this.props.source}</p>
        </div>
      )
    }
    return this.props.children
  }
}
