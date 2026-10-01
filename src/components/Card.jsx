import Gallery from './Gallery.jsx'

export default function Card({ item, kind, title, sub, desc, meta, href, cta, onClick, onTag, index }) {
  const Wrap = href ? 'a' : 'button'
  const wrapProps = href ? { href, target: '_blank', rel: 'noopener noreferrer' } : { onClick, type: 'button' }
  return (
    <article className="card glass" style={{ animationDelay: `${Math.min(index, 8) * 45}ms` }}>
      <div className="card-cover">
        <Gallery item={item} kind={kind} href={href} onClick={onClick} label={title} />
        <span className="card-meta pill-glass">{meta}</span>
      </div>
      <div className="card-body">
        <div className="card-title">
          <h3>{title}</h3>
          <span className="sub">{sub}</span>
        </div>
        <p>{desc}</p>
        <div className="card-foot">
          <div className="tags">
            {item.tags.map((t) => (
              <button key={t} className="pill" onClick={() => onTag(t)}>{t}</button>
            ))}
          </div>
          <Wrap className="link" {...wrapProps}>{cta}</Wrap>
        </div>
      </div>
    </article>
  )
}
