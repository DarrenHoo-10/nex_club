import { useEffect, useRef, useState } from 'react'
import Cover from './Cover.jsx'

// A multi-cover gallery. Uses item.covers (array of image URLs) when provided,
// otherwise generates `item.coverCount` (default 3) cover variants.
export default function Gallery({ item, kind, big = false, href, onClick, autoplay = false, arrows = true, label, activeIndex, onIndexChange }) {
  const covers = Array.isArray(item.covers) && item.covers.length ? item.covers : Array.from({ length: item.coverCount || 3 }, () => null)
  const n = covers.length
  const [localIndex, setLocalIndex] = useState(0)
  const i = activeIndex ?? localIndex
  const setI = onIndexChange || setLocalIndex
  const [hover, setHover] = useState(false)
  const touch = useRef(null)
  const go = (k) => setI(((k % n) + n) % n)

  useEffect(() => {
    if (n < 2 || !(autoplay || hover)) return
    const t = setInterval(() => setI((x) => (x + 1) % n), autoplay ? 3200 : 1700)
    return () => clearInterval(t)
  }, [autoplay, hover, n, setI])

  const View = href ? 'a' : onClick ? 'button' : 'div'
  const viewProps = href
    ? { href, target: '_blank', rel: 'noopener noreferrer', 'aria-label': label }
    : onClick
      ? { onClick, type: 'button', 'aria-label': label }
      : {}

  const stop = (fn) => (e) => {
    e.preventDefault()
    e.stopPropagation()
    fn()
  }

  return (
    <div
      className="gallery"
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      onTouchStart={(e) => (touch.current = e.touches[0].clientX)}
      onTouchEnd={(e) => {
        if (touch.current == null) return
        const dx = e.changedTouches[0].clientX - touch.current
        if (Math.abs(dx) > 40) go(i + (dx < 0 ? 1 : -1))
        touch.current = null
      }}
    >
      <View className="gal-view" {...viewProps}>
        <div className="gal-track" style={{ transform: `translateX(-${i * 100}%)` }}>
          {covers.map((src, k) => (
            <div className="gal-slide" key={k}>
              <Cover item={item} kind={kind} variant={k} big={big} src={src} />
            </div>
          ))}
        </div>
      </View>
      {n > 1 && arrows && (
        <>
          <button className="gal-arrow prev" onClick={stop(() => go(i - 1))} aria-label="上一张封面" tabIndex={-1}>‹</button>
          <button className="gal-arrow next" onClick={stop(() => go(i + 1))} aria-label="下一张封面" tabIndex={-1}>›</button>
        </>
      )}
      {n > 1 && (
        <div className="gal-dots">
          {covers.map((_, k) => (
            <button key={k} className={k === i ? 'gdot on' : 'gdot'} onClick={stop(() => go(k))} aria-label={`第 ${k + 1} 张封面`} tabIndex={-1} />
          ))}
        </div>
      )}
    </div>
  )
}
