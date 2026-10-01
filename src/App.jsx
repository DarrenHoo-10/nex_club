import { useEffect, useMemo, useRef, useState } from 'react'
import tools from './data/tools.json'
import tutorials from './data/tutorials.json'
import repos from './data/repos.json'
import Card from './components/Card.jsx'
import Carousel from './components/Carousel.jsx'
import Reader from './components/Reader.jsx'
import Logo from './components/Logo.jsx'

const host = (url) => new URL(url).hostname.replace('www.', '')

const SORT_OPTIONS = [
  { key: 'recommended', label: '推荐', title: '按编辑推荐顺序' },
  { key: 'heat', label: '热度', title: '按热度从高到低' },
  { key: 'latest', label: '最新', title: '按收录时间从新到旧' },
]

const SECTIONS = [
  {
    key: 'tools',
    label: 'AI工具网站',
    title: '发现真正好用的 AI 工具',
    blurb: '少而精的 AI 工具站点，每一个都值得收藏。',
    kind: 'site',
    items: tools,
    fields: (t) => [t.name, t.desc, ...t.tags],
  },
  {
    key: 'tutorials',
    label: 'AI焚决集合',
    title: '照着做就能成的 AI 秘籍',
    blurb: '一步步的 AI 教程与实战心法，从入门到进阶。',
    kind: 'doc',
    items: tutorials,
    fields: (t) => [t.title, t.summary, t.level, ...t.tags, ...t.steps],
  },
  {
    key: 'repos',
    label: 'AI GitHub 项目',
    title: '值得 Star 的 AI 开源项目',
    blurb: '开源社区里真正有价值的 AI 项目。',
    kind: 'repo',
    items: repos,
    fields: (r) => [r.repo, r.desc, r.lang, ...r.tags],
  },
]

const readHash = () => {
  const key = window.location.hash.replace('#', '')
  return SECTIONS.some((s) => s.key === key) ? key : 'tools'
}

export default function App() {
  const [active, setActive] = useState(readHash)
  const [query, setQuery] = useState('')
  const [tag, setTag] = useState('')
  const [sort, setSort] = useState('recommended')
  const [reading, setReading] = useState(null)
  const searchRef = useRef(null)

  useEffect(() => {
    const onHash = () => setActive(readHash())
    const onKey = (e) => {
      if (e.key === '/' && document.activeElement?.tagName !== 'INPUT') {
        e.preventDefault()
        searchRef.current?.focus()
      }
    }
    window.addEventListener('hashchange', onHash)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('hashchange', onHash)
      window.removeEventListener('keydown', onKey)
    }
  }, [])

  const section = SECTIONS.find((s) => s.key === active)

  const switchSection = (key) => {
    window.location.hash = key
    setQuery('')
    setTag('')
  }

  const allTags = useMemo(() => {
    const counts = new Map()
    section.items.forEach((i) => i.tags.forEach((t) => counts.set(t, (counts.get(t) || 0) + 1)))
    return [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([t]) => t)
  }, [section])

  const results = useMemo(() => {
    const q = query.trim().toLowerCase()
    const filtered = section.items.filter((item) => {
      if (tag && !item.tags.includes(tag)) return false
      return !q || section.fields(item).join(' ').toLowerCase().includes(q)
    })
    if (sort === 'heat') return filtered.sort((a, b) => (b.heat ?? 0) - (a.heat ?? 0))
    if (sort === 'latest') return filtered.sort((a, b) => (Date.parse(b.addedAt) || 0) - (Date.parse(a.addedAt) || 0))
    return filtered
  }, [section, query, tag, sort])

  const itemProps = (item) => {
    if (active === 'tools')
      return { title: item.name, sub: host(item.url), desc: item.desc, meta: item.pricing, href: item.url, cta: '访问 ↗' }
    if (active === 'tutorials')
      return {
        title: item.title, sub: `${item.minutes} 分钟 · ${item.steps.length} 步`, desc: item.summary, meta: item.level,
        onClick: () => setReading(item), cta: '查看教程 →',
      }
    const [owner, name] = item.repo.split('/')
    return { title: name, sub: owner, desc: item.desc, meta: item.lang, href: `https://github.com/${item.repo}`, cta: 'GitHub ↗' }
  }

  const slides = useMemo(
    () =>
      section.items
        .filter((i) => i.featured)
        .map((item) => {
          const p = itemProps(item)
          return { id: item.id, item, title: p.title, desc: p.desc, tags: item.tags, href: p.href, cta: p.cta, onOpen: p.onClick }
        }),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [section],
  )

  const searching = query.trim() !== '' || tag !== ''

  return (
    <div className="page">
      <div className="bg" aria-hidden><i className="b1" /><i className="b2" /><i className="b3" /><i className="b4" /></div>

      <header className="nav glass">
        <a className="logo" href="#tools" onClick={() => switchSection('tools')}>
          <Logo />
          <span className="logo-text">Nex <b>Club</b></span>
        </a>
        <nav className="tabs" aria-label="板块">
          {SECTIONS.map((s) => (
            <button key={s.key} className={s.key === active ? 'tab on' : 'tab'} onClick={() => switchSection(s.key)}>
              {s.label}
            </button>
          ))}
        </nav>
        <span className="nav-count">{tools.length + tutorials.length + repos.length} 项精选</span>
      </header>

      <main key={active}>
        <section className="hero">
          <h1>{section.title.replace(/(AI)/, '§$1§').split('§').map((p, i) => (p === 'AI' ? <span key={i} className="grad">AI</span> : p))}</h1>
          <p>{section.blurb}</p>
          <label className="search glass">
            <span aria-hidden>⌕</span>
            <input
              ref={searchRef}
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={`在「${section.label}」中搜索…`}
              aria-label="搜索"
            />
            <kbd>/</kbd>
          </label>
        </section>

        {!searching && slides.length > 0 && <Carousel slides={slides} kind={section.kind} />}

        <div className="filters" role="group" aria-label="标签筛选">
          <button className={tag === '' ? 'pill on' : 'pill'} onClick={() => setTag('')}>全部</button>
          {allTags.map((t) => (
            <button key={t} className={tag === t ? 'pill on' : 'pill'} onClick={() => setTag(tag === t ? '' : t)}>{t}</button>
          ))}
        </div>

        <div className="list-toolbar">
          <h2 id="list-title" className="list-title" aria-live="polite">{searching ? `找到 ${results.length} 项` : `全部${section.label}`}</h2>
          <div className="sort-control pill-glass" role="group" aria-label="排序方式">
            {SORT_OPTIONS.map((option) => (
              <button
                key={option.key}
                type="button"
                className={sort === option.key ? 'sort-button on' : 'sort-button'}
                aria-pressed={sort === option.key}
                title={option.title}
                onClick={() => setSort(option.key)}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>
        <section className="grid" aria-labelledby="list-title">
          {results.map((item, i) => (
            <Card key={item.id} item={item} kind={section.kind} index={i} onTag={setTag} {...itemProps(item)} />
          ))}
        </section>
        {results.length === 0 && <p className="empty">没有找到匹配的内容，换个关键词试试。</p>}
      </main>

      <footer className="footer">Nex Club · 原型演示 · 热度与收录时间为示例数据</footer>
      {reading && <Reader item={reading} onClose={() => setReading(null)} />}
    </div>
  )
}
