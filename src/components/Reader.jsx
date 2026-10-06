import MarkdownBody from './MarkdownBody.jsx'

export default function Reader({ item, showTitle = true }) {
  const steps = Array.isArray(item.steps) ? item.steps : []
  const stepLabel = steps.length ? ` · ${steps.length} 步` : ''
  const authorLabel = item.author ? ` · ${item.author}` : ''
  return (
    <article className="reader glass strong" aria-label={item.title}>
      {item.cover ? (
        <div className="reader-cover">
          <img src={item.cover} alt="" loading="lazy" />
        </div>
      ) : null}
      <p className="eyebrow">{item.levelLabel} · 约 {item.minutes} 分钟{stepLabel}{authorLabel}</p>
      {showTitle && <h2>{item.title}</h2>}
      {item.bodyMarkdown ? <MarkdownBody source={item.bodyMarkdown} /> : null}
      {steps.length > 0 && (
        <ol className="reader-steps">
          {steps.map((step, index) => (
            <li key={index}>
              <span>{String(index + 1).padStart(2, '0')}</span>
              <p>{step}</p>
            </li>
          ))}
        </ol>
      )}
      {item.notes ? (
        <aside className="reader-note">
          <strong>注意</strong>
          <p>{item.notes}</p>
        </aside>
      ) : null}
    </article>
  )
}
