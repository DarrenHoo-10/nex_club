import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import Card from '../components/Card.jsx'
import Carousel from '../components/Carousel.jsx'
import ResourceModal from '../components/ResourceModal.jsx'
import { useApi } from '../api/context.jsx'
import { toCardModel, toGalleryKind } from '../api/view.js'
import { RELEVANCE_OPTION, SORT_OPTIONS, sectionByKey } from '../sections.js'
import { useResourceList } from './useResourceList.js'

export default function SectionPage({ sectionKey }) {
  const section = sectionByKey(sectionKey)
  const client = useApi()
  const [params, setParams] = useSearchParams()
  const qRaw = params.get('q') || ''
  const q = qRaw.trim()
  const tag = params.get('tag') || ''
  const sort = params.get('sort') || ''
  const list = useResourceList(client, { kind: section.kind, q, tag, sort })
  const [tags, setTags] = useState([])
  const [featured, setFeatured] = useState([])
  const [selected, setSelected] = useState(null)
  const searching = q !== '' || tag !== ''
  const galleryKind = toGalleryKind(section.kind)

  useEffect(() => {
    const ctrl = new AbortController()
    client.listTags({ kind: section.kind, q }, { signal: ctrl.signal })
      .then((body) => setTags(body.tags || []))
      .catch((err) => {
        if (err?.name === 'AbortError') return
        setTags([])
      })
    return () => ctrl.abort()
  }, [client, section.kind, q])

  useEffect(() => {
    if (searching) {
      setFeatured([])
      return undefined
    }
    const ctrl = new AbortController()
    client.listFeatured(section.kind, { signal: ctrl.signal })
      .then((body) => setFeatured((body.items || []).map(toCardModel)))
      .catch((err) => {
        if (err?.name === 'AbortError') return
        setFeatured([])
      })
    return () => ctrl.abort()
  }, [client, section.kind, searching])

  const setQuery = (next) => {
    setParams((prev) => {
      const paramsNext = new URLSearchParams(prev)
      if (next) paramsNext.set('q', next)
      else paramsNext.delete('q')
      return paramsNext
    }, { replace: true })
  }

  const setTag = (slug) => {
    setParams((prev) => {
      const paramsNext = new URLSearchParams(prev)
      if (slug) paramsNext.set('tag', slug)
      else paramsNext.delete('tag')
      return paramsNext
    }, { replace: true })
  }

  const setSort = (next) => {
    setParams((prev) => {
      const paramsNext = new URLSearchParams(prev)
      paramsNext.set('sort', next)
      return paramsNext
    })
  }

  const options = q || list.effectiveSort === 'relevance' ? [...SORT_OPTIONS, RELEVANCE_OPTION] : SORT_OPTIONS
  const heading = listHeading(section, searching, list.total, list.items.length)
  const slides = useMemo(() => featured.map((model) => ({
    id: model.id,
    item: model,
    title: model.title,
    desc: model.desc,
    tags: model.tags,
    onOpen: () => setSelected(model),
  })), [featured])

  return (
    <main>
      <section className="hero">
        <h1>{section.title.split(/(AI)/).map((part, index) => (part === 'AI' ? <span key={index} className="grad">AI</span> : part))}</h1>
        <p>{section.blurb}</p>
        <label className="search glass">
          <span aria-hidden>⌕</span>
          <input
            id="site-search"
            type="search"
            value={qRaw}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={`在「${section.label}」中搜索…`}
            aria-label="搜索"
          />
          <kbd>/</kbd>
        </label>
      </section>

      {!searching && slides.length > 0 && <Carousel slides={slides} kind={galleryKind} suspended={!!selected} />}

      <div className="filters" role="group" aria-label="标签筛选">
        <button type="button" className={tag === '' ? 'pill on' : 'pill'} onClick={() => setTag('')}>全部</button>
        {tags.map((entry) => (
          <button
            key={entry.slug}
            type="button"
            className={tag === entry.slug ? 'pill on' : 'pill'}
            onClick={() => setTag(tag === entry.slug ? '' : entry.slug)}
          >
            {entry.name}
          </button>
        ))}
      </div>

      <div className="list-toolbar">
        <h2 id="list-title" className="list-title" aria-live="polite">{heading}</h2>
        <div className="sort-control pill-glass" role="group" aria-label="排序方式">
          {options.map((option) => (
            <button
              key={option.key}
              type="button"
              className={list.effectiveSort === option.key ? 'sort-button on' : 'sort-button'}
              aria-pressed={list.effectiveSort === option.key}
              title={option.title}
              onClick={() => setSort(option.key)}
            >
              {option.label}
            </button>
          ))}
        </div>
      </div>

      <section className="grid" aria-labelledby="list-title" aria-busy={list.loading}>
        {list.items.map((model, index) => (
          <Card
            key={model.id}
            item={model}
            kind={galleryKind}
            index={index}
            title={model.title}
            sub={model.subtitle}
            desc={model.desc}
            meta={model.meta}
            onTag={setTag}
            onClick={() => setSelected(model)}
          />
        ))}
      </section>
      {list.loading && list.items.length === 0 && !list.error && <p className="empty" aria-busy="true">加载中…</p>}
      {list.error && (
        <div className="alert" role="alert">
          <p>列表加载失败。</p>
          <button type="button" className="pill" onClick={list.reload}>重试</button>
        </div>
      )}
      {!list.error && !list.loading && list.items.length === 0 && <p className="empty">没有找到匹配的内容，换个关键词试试。</p>}
      {list.hasMore && !list.error && (
        <div className="load-more">
          <button type="button" className="pill" onClick={list.loadMore} disabled={list.loading}>加载更多</button>
        </div>
      )}
      {selected && <ResourceModal key={selected.slug} item={selected} onClose={() => setSelected(null)} />}
    </main>
  )
}

function listHeading(section, searching, total, count) {
  if (!searching) return `全部${section.label}`
  if (typeof total === 'number') return `找到 ${total} 项`
  return `找到 ${count} 项`
}
