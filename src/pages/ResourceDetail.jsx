import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import Gallery from '../components/Gallery.jsx'
import Reader from '../components/Reader.jsx'
import { useApi } from '../api/context.jsx'
import { toCardModel, toGalleryKind, toReaderModel } from '../api/view.js'
import { SECTIONS, sectionByKind } from '../sections.js'
import { openOutbound } from './outbound.js'

export default function ResourceDetail() {
  const { slug } = useParams()
  const client = useApi()
  const [resource, setResource] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(true)
  const [attempt, setAttempt] = useState(0)
  const seen = useRef(null)

  useEffect(() => {
    const ctrl = new AbortController()
    setLoading(true)
    setError(null)
    client.getBySlug(slug, { signal: ctrl.signal })
      .then((data) => {
        setResource(data)
        setLoading(false)
      })
      .catch((err) => {
        if (err?.name === 'AbortError') return
        setResource(null)
        setError(err)
        setLoading(false)
      })
    return () => ctrl.abort()
  }, [client, slug, attempt])

  useEffect(() => {
    if (!resource || seen.current === `${slug}:${resource.id}`) return
    seen.current = `${slug}:${resource.id}`
    const id = crypto.randomUUID()
    client.postEvents([{ id, resource_id: resource.id, type: 'detail_view' }]).catch(() => {})
  }, [client, resource, slug])

  if (loading && !resource) return <main className="detail" aria-busy="true"><p className="empty">加载中…</p></main>
  if (isNotFound(error)) {
    return (
      <main className="detail">
        <h1>这份内容已经下架或不存在</h1>
        <nav className="back-links" aria-label="返回板块">
          {SECTIONS.map((section) => <Link key={section.key} className="pill" to={`/${section.key}`}>{section.label}</Link>)}
        </nav>
      </main>
    )
  }
  if (error) {
    return (
      <main className="detail">
        <div className="alert" role="alert">
          <p>内容加载失败。</p>
          <button type="button" className="pill" onClick={() => setAttempt((value) => value + 1)}>重试</button>
        </div>
      </main>
    )
  }

  const section = sectionByKind(resource.kind)
  if (resource.kind === 'tutorial') {
    return (
      <main className="detail" aria-busy={loading}>
        <Link className="pill" to={`/${section.key}`}>返回{section.label}</Link>
        <Reader item={toReaderModel(resource)} />
        {resource.recommendation_reason ? <p>{resource.recommendation_reason}</p> : null}
      </main>
    )
  }

  const model = toCardModel(resource)
  return (
    <main className="detail" aria-busy={loading}>
      <Link className="pill" to={`/${section.key}`}>返回{section.label}</Link>
      <div className="detail-cover">
        <Gallery item={model} kind={toGalleryKind(resource.kind)} label={model.title} />
      </div>
      <h1>{resource.title}</h1>
      <p className="detail-lead">{resource.summary}</p>
      <div className="tags">
        {model.tags.map((name, index) => (
          <Link key={model.tagSlugs[index] || name} className="pill" to={`/${section.key}?tag=${encodeURIComponent(model.tagSlugs[index] || name)}`}>{name}</Link>
        ))}
      </div>
      {model.href ? (
        <a className="btn primary" href={model.href} onClick={(event) => { event.preventDefault(); openOutbound(client, resource.id, model.href) }}>{model.cta}</a>
      ) : null}
      {resource.recommendation_reason ? <p>{resource.recommendation_reason}</p> : null}
    </main>
  )
}

function isNotFound(error) {
  return error?.status === 404 || error?.code === 'not_found'
}
