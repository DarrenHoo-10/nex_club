import { Component } from 'react'
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
            img({ src, alt }) {
              const safe = safeHttpUrl(src)
              if (!safe && !/^\/(?!\/)/.test(src || '')) return null
              return <img src={safe || src} alt={alt || ''} loading="lazy" />
            },
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
