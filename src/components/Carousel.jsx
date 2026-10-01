import { useCallback, useEffect, useRef, useState } from 'react'
import Gallery from './Gallery.jsx'

export default function Carousel({ slides, kind }) {
  const [i, setI] = useState(0)
  const [paused, setPaused] = useState(false)
  const n = slides.length
  const touch = useRef(null)

  const go = useCallback((next) => setI(((next % n) + n) % n), [n])

  useEffect(() => {
    setI(0)
  }, [slides])

  useEffect(() => {
    if (paused || n < 2) return
    const t = setInterval(() => setI((x) => (x + 1) % n), 5200)
    return () => clearInterval(t)
  }, [paused, n])

  if (!n) return null

  return (
    <section
      className="carousel"
      aria-roledescription="carousel"
      aria-label="推荐"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onTouchStart={(e) => (touch.current = e.touches[0].clientX)}
      onTouchEnd={(e) => {
        if (touch.current == null) return
        const dx = e.changedTouches[0].clientX - touch.current
        if (Math.abs(dx) > 40) go(i + (dx < 0 ? 1 : -1))
        touch.current = null
      }}
    >
      <div className="car-viewport">
        <div className="car-track" style={{ '--i': i }}>
          {slides.map((s, idx) => (
            <article
              key={s.id}
              className={idx === i ? 'slide glass active' : 'slide glass'}
              onClick={() => idx !== i && go(idx)}
              aria-hidden={idx !== i}
            >
              <div className="slide-text">
                <span className="slide-kicker">编辑推荐 · {String(idx + 1).padStart(2, '0')}/{String(n).padStart(2, '0')}</span>
                <h3>{s.title}</h3>
                <p>{s.desc}</p>
                <div className="slide-tags">{s.tags.map((t) => <span key={t} className="pill">{t}</span>)}</div>
                {s.href ? (
                  <a className="btn primary" href={s.href} target="_blank" rel="noopener noreferrer" tabIndex={idx === i ? 0 : -1}>
                    {s.cta}
                  </a>
                ) : (
                  <button className="btn primary" onClick={s.onOpen} tabIndex={idx === i ? 0 : -1}>{s.cta}</button>
                )}
              </div>
              <div className="slide-art">
                <Gallery item={s.item} kind={kind} big autoplay={idx === i} arrows={false} />
              </div>
            </article>
          ))}
        </div>
      </div>
      <button className="car-nav prev glass" onClick={() => go(i - 1)} aria-label="上一张">‹</button>
      <button className="car-nav next glass" onClick={() => go(i + 1)} aria-label="下一张">›</button>
      <div className="dots">
        {slides.map((s, idx) => (
          <button key={s.id} className={idx === i ? 'dot on' : 'dot'} onClick={() => go(idx)} aria-label={`第 ${idx + 1} 张`} />
        ))}
      </div>
    </section>
  )
}
