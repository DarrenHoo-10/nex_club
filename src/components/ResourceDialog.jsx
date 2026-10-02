import { useEffect, useId, useRef } from 'react'
import { createPortal } from 'react-dom'

export default function ResourceDialog({ title, eyebrow, onClose, closeLabel = '收起', toolbar, busy = false, children }) {
  const dialog = useRef(null)
  const titleId = useId()

  useEffect(() => {
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

  return createPortal(
    <dialog
      ref={dialog}
      className="resource-modal glass strong"
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
      <div className="resource-modal-content" aria-busy={busy}>
        <h2 id={titleId}>{title}</h2>
        {children}
      </div>
    </dialog>,
    document.body,
  )
}
