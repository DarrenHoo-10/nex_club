import { useLayoutEffect, useRef, useState } from 'react'
import { readPosition, writePosition } from './readingPosition.js'

export function useReadingNavigation(contentRef, { enabled, storageKey, contentVersion }) {
  const [headings, setHeadings] = useState([])
  const [active, setActive] = useState(null)
  const jump = useRef(() => {})

  useLayoutEffect(() => {
    const root = contentRef.current
    if (!enabled || !root) { setHeadings([]); return }
    const nodes = [...root.querySelectorAll('.reader-body :is(h1, h2, h3, h4, h5, h6)[id]')]
      .filter((node) => node.textContent.trim() !== '目录')
    const minimum = Math.min(...nodes.map((node) => Number(node.tagName[1])))
    setHeadings(nodes.map((node) => ({ id: node.id, text: node.textContent, depth: Number(node.tagName[1]) - minimum })))
    const saved = storageKey && readPosition(storageKey)
    let restoring = Boolean(saved)
    let frame = 0
    let lastPosition = saved
    const relativeTop = (node) => node.getBoundingClientRect().top - root.getBoundingClientRect().top
    const update = () => {
      frame = 0
      const current = nodes.filter((node) => relativeTop(node) <= 32).at(-1)
      setActive(current?.id || null)
      lastPosition = { heading: current?.id || null, offset: current ? relativeTop(current) : 0, top: root.scrollTop }
      if (storageKey && !restoring) writePosition(storageKey, lastPosition)
    }
    const restore = () => {
      const target = nodes.find((node) => node.id === saved?.heading)
      if (target) root.scrollTop += relativeTop(target) - (Number.isFinite(saved.offset) ? saved.offset : 24)
      else root.scrollTop = Number.isFinite(saved?.top) ? saved.top : 0
      update()
    }
    const onScroll = () => { if (!frame) frame = requestAnimationFrame(update) }
    const stopRestoring = () => { restoring = false; update() }
    const onLoad = () => { if (restoring) restore() }
    jump.current = (id) => {
      restoring = false
      const target = nodes.find((node) => node.id === id)
      if (target) {
        root.scrollTop += relativeTop(target) - 24
        target.focus({ preventScroll: true })
      } else {
        root.scrollTop = 0
        root.querySelector('h2')?.focus({ preventScroll: true })
      }
      update()
    }
    const onAnchor = (event) => {
      const link = event.target.closest('a[href^="#"]')
      if (!link || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
      let id
      try { id = decodeURIComponent(link.getAttribute('href').slice(1)) } catch { return }
      if (!nodes.some((node) => node.id === id)) return
      event.preventDefault()
      jump.current(id)
    }
    restore()
    const timer = setTimeout(stopRestoring, 1500)
    root.addEventListener('scroll', onScroll, { passive: true })
    root.addEventListener('load', onLoad, true)
    root.addEventListener('wheel', stopRestoring, { passive: true })
    root.addEventListener('touchstart', stopRestoring, { passive: true })
    root.addEventListener('keydown', stopRestoring)
    root.addEventListener('click', onAnchor)
    window.addEventListener('pagehide', stopRestoring)
    return () => {
      clearTimeout(timer)
      cancelAnimationFrame(frame)
      root.removeEventListener('scroll', onScroll)
      root.removeEventListener('load', onLoad, true)
      root.removeEventListener('wheel', stopRestoring)
      root.removeEventListener('touchstart', stopRestoring)
      root.removeEventListener('keydown', stopRestoring)
      root.removeEventListener('click', onAnchor)
      window.removeEventListener('pagehide', stopRestoring)
      // Store live container geometry before the modal leaves the DOM.
      if (root.isConnected && root.getBoundingClientRect().height > 0) { restoring = false; update() }
      else if (storageKey && lastPosition) writePosition(storageKey, lastPosition)
      jump.current = () => {}
    }
  }, [contentRef, enabled, storageKey, contentVersion])

  return { headings, active, goTo: (id) => jump.current(id) }
}

export default function ArticleOutline({ headings, active, onNavigate }) {
  const nav = useRef(null)
  useLayoutEffect(() => {
    const root = nav.current
    const current = root?.querySelector('[aria-current]')
    if (!current || !root.clientHeight) return
    const relative = current.getBoundingClientRect().top - root.getBoundingClientRect().top
    if (relative < 0) root.scrollTop += relative - 8
    else if (relative + current.offsetHeight > root.clientHeight) root.scrollTop += relative + current.offsetHeight - root.clientHeight + 8
  }, [active])
  return <nav ref={nav} className="reader-outline" aria-label="章节目录">
    <a href="#" aria-current={active === null ? 'location' : undefined} onClick={(event) => { event.preventDefault(); onNavigate(null) }}>文章开头</a>
    {headings.map((heading) => <a
      key={heading.id}
      href={`#${encodeURIComponent(heading.id)}`}
      className={heading.depth > 0 ? 'reader-outline-subsection' : undefined}
      aria-current={active === heading.id ? 'location' : undefined}
      onClick={(event) => { event.preventDefault(); onNavigate(heading.id) }}
    >{heading.text}</a>)}
  </nav>
}
