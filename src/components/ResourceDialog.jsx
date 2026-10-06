import { useId, useLayoutEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import ArticleOutline, { useReadingNavigation } from './ArticleOutline.jsx'

export default function ResourceDialog({ title, eyebrow, onClose, closeLabel = '收起', toolbar, busy = false, reading = false, readingKey, contentVersion, children }) {
  const dialog = useRef(null)
  const content = useRef(null)
  const titleId = useId()
  const mobileOutline = useRef(null)

  useLayoutEffect(() => {
    const node = dialog.current
    const trigger = document.activeElement
    const overflow = document.body.style.overflow
    node.showModal()
    document.body.style.overflow = 'hidden'
    return () => {
      node.close()
      document.body.style.overflow = overflow
      if (trigger?.isConnected) trigger.focus({ preventScroll: true })
    }
  }, [])

  const navigation = useReadingNavigation(content, { enabled: reading && !busy, storageKey: readingKey, contentVersion })
  const goTo = (id) => {
    if (mobileOutline.current) mobileOutline.current.open = false
    navigation.goTo(id)
  }
  const hasOutline = reading && navigation.headings.length > 0

  return createPortal(
    <dialog
      ref={dialog}
      className={`resource-modal glass strong${reading ? ' resource-modal-reading' : ''}${hasOutline ? ' resource-modal-outlined' : ''}`}
      aria-labelledby={titleId}
      onCancel={(event) => { event.preventDefault(); onClose() }}
      onClick={(event) => {
        if (event.target !== event.currentTarget) return
        const rect = event.currentTarget.getBoundingClientRect()
        if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) onClose()
      }}
    >
      <div className="resource-modal-head">
        <span className="eyebrow">{eyebrow}</span>
        <button type="button" className="pill resource-modal-close" onClick={onClose}><span aria-hidden="true">×</span>{closeLabel}</button>
      </div>
      {toolbar}
      {hasOutline && <details className="reader-mobile-outline" ref={mobileOutline}>
        <summary><span>章节目录</span><span className="reader-current-chapter">{navigation.headings.find((heading) => heading.id === navigation.active)?.text || '文章开头'}</span><span aria-hidden="true">⌄</span></summary>
        <ArticleOutline {...navigation} onNavigate={goTo} />
      </details>}
      <div ref={content} className="resource-modal-content" aria-busy={busy}>
        <h2 id={titleId} tabIndex={-1}>{title}</h2>
        {children}
      </div>
      {hasOutline && <aside className="reader-side-outline glass strong">
        <div className="reader-outline-label">章节目录</div>
        <ArticleOutline {...navigation} onNavigate={goTo} />
        <button type="button" className="reader-outline-top" onClick={() => goTo(null)}>回到开头 ↑</button>
      </aside>}
    </dialog>,
    document.body,
  )
}
