import { useEffect, useRef, useState } from 'react'
import { useApi } from '../api/context.jsx'
import ResourceContent from './ResourceContent.jsx'
import ResourceDialog from './ResourceDialog.jsx'

const KIND_LABELS = { tool: 'AI 工具', tutorial: '实用教程', repo: '开源项目' }

export default function ResourceModal({ item, onClose }) {
  const client = useApi()
  const seen = useRef(false)
  const [resource, setResource] = useState(null)
  const [error, setError] = useState(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const ctrl = new AbortController()
    setError(null)
    client.getBySlug(item.slug, { signal: ctrl.signal }).then((data) => {
      if (ctrl.signal.aborted) return
      setResource(data)
      if (!seen.current) {
        seen.current = true
        client.postEvents([{ id: crypto.randomUUID(), resource_id: data.id, type: 'detail_view' }]).catch(() => {})
      }
    }).catch((err) => {
      if (!ctrl.signal.aborted) setError(err)
    })
    return () => ctrl.abort()
  }, [client, item.slug, attempt])

  const unavailable = error?.status === 404 || error?.code === 'not_found'
  return <ResourceDialog
    title={resource?.title || item.title}
    eyebrow={`${KIND_LABELS[resource?.kind || item.kind]} · 详情`}
    onClose={onClose}
    busy={!resource && !error}
    reading={(resource?.kind || item.kind) === 'tutorial'}
    readingKey={`tutorial:${item.slug}`}
    contentVersion={resource}
  >
    {!resource && !error && <p className="empty" role="status">正在加载介绍…</p>}
    {error && <div className="alert" role="alert">
      <p>{unavailable ? '这份内容已经下架或不存在' : '介绍加载失败，请重试。'}</p>
      {!unavailable && <button type="button" className="pill" onClick={() => setAttempt((value) => value + 1)}>重试</button>}
    </div>}
    {resource && <ResourceContent resource={resource} onOutbound={() => {
      client.postEvents([{ id: crypto.randomUUID(), resource_id: resource.id, type: 'outbound_click' }]).catch(() => {})
    }} />}
  </ResourceDialog>
}
