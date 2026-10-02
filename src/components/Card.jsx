import { useRef } from 'react'
import Gallery from './Gallery.jsx'

export default function Card({ item, kind, title, sub, desc, meta, onClick, onTag, index = 0 }) {
  const trigger = useRef(null)
  const tags = (item?.tags || []).map((tag, i) => (
    typeof tag === 'string'
      ? { name: tag, slug: item.tagSlugs?.[i] || tag }
      : { name: tag.name, slug: tag.slug || tag.name }
  ))
  const open = () => {
    trigger.current?.focus({ preventScroll: true })
    onClick?.()
  }
  return (
    <article
      className={`card glass${onClick ? ' card-interactive' : ''}`}
      style={{ animationDelay: `${Math.min(index, 8) * 45}ms` }}
      onClick={onClick ? (event) => {
        if (!event.target.closest('button, a')) open()
      } : undefined}
    >
      <div className="card-cover">
        <Gallery item={item} kind={kind} label={title} />
        <span className="card-meta pill-glass">{meta}</span>
      </div>
      <div className="card-body">
        <div className="card-title">
          <h3>{onClick ? <button ref={trigger} type="button" className="card-open" aria-haspopup="dialog" onClick={open}>{title}</button> : title}</h3>
          <span className="sub">{sub}</span>
        </div>
        <p>{desc}</p>
        {tags.length > 0 && <div className="card-foot">
          <div className="tags">
            {tags.map((tag, i) => (
              onTag ? <button
                key={`${tag.slug}-${i}`}
                type="button"
                className="pill"
                onClick={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  onTag?.(tag.slug)
                }}
              >
                {tag.name}
              </button> : <span key={`${tag.slug}-${i}`} className="pill">{tag.name}</span>
            ))}
          </div>
        </div>}
      </div>
    </article>
  )
}
