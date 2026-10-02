import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { KIND_LABEL, STATUS_LABEL } from '../labels.js'
import { useAdminSession } from '../session.jsx'

export default function ResourceListPage() {
  const { api } = useAdminSession()
  const [params, setParams] = useSearchParams()
  const kind = params.get('kind') || ''
  const status = params.get('status') || ''
  const q = params.get('q') || ''
  const [items, setItems] = useState([])
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    setLoading(true)
    api.listResources({ kind, status, q })
      .then((body) => {
        if (!alive) return
        setItems(body.items || [])
        setError(null)
        setLoading(false)
      })
      .catch((err) => {
        if (!alive || err?.status === 401) return
        setError(err)
        setLoading(false)
      })
    return () => { alive = false }
  }, [api, kind, status, q])

  const setParam = (name, value) => {
    setParams((prev) => {
      const next = new URLSearchParams(prev)
      if (value) next.set(name, value)
      else next.delete(name)
      return next
    }, { replace: true })
  }

  return (
    <section className="admin-block glass" aria-busy={loading}>
      <div className="row-actions">
        <h1>资源</h1>
        <Link className="pill" to="/admin/resources/new?kind=tool">新建工具</Link>
        <Link className="pill" to="/admin/resources/new?kind=tutorial">新建教程</Link>
        <Link className="pill" to="/admin/resources/new?kind=repo">新建仓库</Link>
      </div>
      <div className="row-actions">
        <label className="field">类型
          <select value={kind} onChange={(event) => setParam('kind', event.target.value)}>
            <option value="">全部</option>
            {Object.entries(KIND_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <label className="field">状态
          <select aria-label="状态" value={status} onChange={(event) => setParam('status', event.target.value)}>
            <option value="">全部</option>
            {Object.entries(STATUS_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <label className="field">标题
          <input aria-label="标题关键词" value={q} onChange={(event) => setParam('q', event.target.value)} />
        </label>
      </div>
      {error ? <p role="alert">{error.message || '加载失败'}</p> : null}
      <table className="admin-table">
        <thead>
          <tr>
            <th>标题</th>
            <th>类型</th>
            <th>状态</th>
            <th>版本</th>
            <th>未发布草稿</th>
            <th>更新时间</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id}>
              <td><Link to={`/admin/resources/${item.id}`}>{item.title}</Link></td>
              <td>{KIND_LABEL[item.kind] || item.kind}</td>
              <td>{STATUS_LABEL[item.status] || item.status}</td>
              <td>{item.edit_version}</td>
              <td>{item.has_unpublished_draft ? '有' : '无'}</td>
              <td>{item.updated_at}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {!loading && items.length === 0 ? <p className="empty">没有资源。</p> : null}
    </section>
  )
}
