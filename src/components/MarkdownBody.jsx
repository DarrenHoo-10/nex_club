import { Component } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { safeHttpUrl } from '../api/view.js'

const NOTICE = '正文格式无法完整解析，以下为原文。'

export default function MarkdownBody({ source }) {
  return (
    <MarkdownErrorBoundary source={source}>
      <div className="reader-body">
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          urlTransform={safeHttpUrl}
          components={{
            a({ href, children }) {
              const safe = safeHttpUrl(href)
              if (!safe) return <span>{children}</span>
              return <a href={safe} target="_blank" rel="nofollow noopener noreferrer">{children}</a>
            },
            img({ src, alt }) {
              const safe = safeHttpUrl(src)
              if (!safe && !src?.startsWith('/')) return null
              return <img src={safe || src} alt={alt || ''} loading="lazy" />
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
