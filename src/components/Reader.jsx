import { useEffect } from 'react'

export default function Reader({ item, onClose }) {
  useEffect(() => {
    const onKey = (e) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [onClose])

  return (
    <div className="reader-wrap" onClick={onClose}>
      <article className="reader glass strong" role="dialog" aria-modal="true" aria-label={item.title} onClick={(e) => e.stopPropagation()}>
        <button className="reader-close pill-glass" onClick={onClose}>关闭 <kbd>Esc</kbd></button>
        <p className="eyebrow">{item.level} · 约 {item.minutes} 分钟 · {item.steps.length} 步</p>
        <h2>{item.title}</h2>
        <p className="reader-lead">{item.summary}</p>
        <ol className="reader-steps">
          {item.steps.map((s, i) => (
            <li key={i}>
              <span>{String(i + 1).padStart(2, '0')}</span>
              <p>{s}</p>
            </li>
          ))}
        </ol>
        {item.notes && (
          <aside className="reader-note">
            <strong>注意</strong>
            <p>{item.notes}</p>
          </aside>
        )}
      </article>
    </div>
  )
}
