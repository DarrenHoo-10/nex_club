import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Gallery from './Gallery.jsx'

export default function Carousel({ slides, kind, suspended = false }) {
  // A different recommendation set starts a new loop at its first item.
  return <CarouselLoop key={slides.map((slide) => slide.id).join('|')} slides={slides} kind={kind} suspended={suspended} />
}

function CarouselLoop({ slides, kind, suspended }) {
  const n = slides.length
  const [position, setPosition] = useState(n > 1 ? n : 0)
  const [moving, setMoving] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [paused, setPaused] = useState(false)
  const [focused, setFocused] = useState(false)
  const [coverIndices, setCoverIndices] = useState({})
  const busy = useRef(false)
  const touch = useRef(null)
  const i = n ? position % n : 0
  const loopSlides = n > 1 ? [...slides, ...slides, ...slides] : slides
  // All visual copies share cover state so rebasing never changes the artwork.
  const coverSetters = useMemo(() => new Map(slides.map((slide) => [slide.id, (next) => {
    setCoverIndices((current) => ({
      ...current,
      [slide.id]: typeof next === 'function' ? next(current[slide.id] || 0) : next,
    }))
  }])), [slides])

  const move = useCallback((delta) => {
    if (n < 2 || !delta || busy.current) return
    busy.current = true
    setMoving(true)
    setPosition((current) => current + delta)
  }, [n])

  const finish = useCallback(() => {
    if (!busy.current || resetting) return
    if (position < n || position >= n * 2) {
      // The adjacent copy is now centered. Rebase to its identical middle copy
      // without animation, preserving the direction and distance of the move.
      setResetting(true)
      setPosition(n + (position % n))
    } else {
      busy.current = false
      setMoving(false)
    }
  }, [n, position, resetting])

  useEffect(() => {
    if (!resetting) return undefined
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => {
        setResetting(false)
        setMoving(false)
        busy.current = false
      })
    })
    return () => cancelAnimationFrame(frame)
  }, [resetting])

  useEffect(() => {
    if (!moving || resetting) return undefined
    // Also release the loop if reduced motion or a hidden tab skips transitionend.
    const timer = setTimeout(finish, 750)
    return () => clearTimeout(timer)
  }, [moving, resetting, finish])

  useEffect(() => {
    if (paused || focused || suspended || n < 2) return undefined
    const t = setInterval(() => move(1), 5200)
    return () => clearInterval(t)
  }, [paused, focused, suspended, n, move])

  const goTo = (index) => {
    let delta = index - i
    if (delta > n / 2) delta -= n
    if (delta < -n / 2) delta += n
    move(delta)
  }

  if (!n) return null

  return (
    <section
      className="carousel"
      aria-roledescription="carousel"
      aria-label="推荐"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocusCapture={() => setFocused(true)}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false)
      }}
      onTouchStart={(e) => (touch.current = e.touches[0].clientX)}
      onTouchEnd={(e) => {
        if (touch.current == null) return
        const dx = e.changedTouches[0].clientX - touch.current
        if (Math.abs(dx) > 40) move(dx < 0 ? 1 : -1)
        touch.current = null
      }}
    >
      <div className="car-viewport">
        <div
          className={`car-track${resetting ? ' car-track-reset' : ''}`}
          style={{ '--i': position }}
          onTransitionEnd={(event) => {
            if (event.target === event.currentTarget && event.propertyName === 'transform') finish()
          }}
        >
          {loopSlides.map((s, idx) => (
            <article
              key={`${s.id}-${idx}`}
              className={idx === position ? 'slide glass active' : 'slide glass'}
              onClick={(event) => {
                if (event.target.closest('button, a')) return
                if (idx !== position) move(idx - position)
                else if (!busy.current) {
                  event.currentTarget.querySelector('.card-open')?.focus({ preventScroll: true })
                  s.onOpen()
                }
              }}
              aria-hidden={idx !== position}
            >
              <div className="slide-text">
                <span className="slide-kicker">编辑推荐 · {String(idx % n + 1).padStart(2, '0')}/{String(n).padStart(2, '0')}</span>
                <h3><button className="card-open" type="button" aria-haspopup="dialog" onClick={() => {
                  if (idx !== position) move(idx - position)
                  else if (!busy.current) s.onOpen()
                }} tabIndex={idx === position ? 0 : -1}>{s.title}</button></h3>
                <p>{s.desc}</p>
                <div className="slide-tags">{s.tags.map((t) => <span key={t} className="pill">{t}</span>)}</div>
              </div>
              <div className="slide-art">
                <Gallery item={s.item} kind={kind} big autoplay={idx === position && !suspended} arrows={false} activeIndex={coverIndices[s.id] || 0} onIndexChange={coverSetters.get(s.id)} />
              </div>
            </article>
          ))}
        </div>
      </div>
      <button className="car-nav prev glass" type="button" onClick={() => move(-1)} disabled={moving || n < 2} aria-label="上一张">‹</button>
      <button className="car-nav next glass" type="button" onClick={() => move(1)} disabled={moving || n < 2} aria-label="下一张">›</button>
      <div className="dots">
        {slides.map((s, idx) => (
          <button key={s.id} className={idx === i ? 'dot on' : 'dot'} type="button" onClick={() => goTo(idx)} disabled={moving} aria-current={idx === i ? 'true' : undefined} aria-label={`第 ${idx + 1} 张`} />
        ))}
      </div>
    </section>
  )
}
