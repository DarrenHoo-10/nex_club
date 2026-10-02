import { useEffect, useState } from 'react'
import { fromUtcInput } from '../editorState.js'
import { KIND_LABEL } from '../labels.js'
import { useAdminSession } from '../session.jsx'

const EMPTY = { kind: 'tool', position: 1, resource_slug: '', starts_at: '', ends_at: '' }

export default function FeaturedPage() {
  const { api } = useAdminSession()
  const [items, setItems] = useState([])
  const [form, setForm] = useState(EMPTY)
  const [error, setError] = useState('')

  const reload = () => api.listFeatured().then((body) => setItems(body.items || [])).catch((err) => setError(err.message || '加载失败'))

  useEffect(() => { reload() }, [api])

  async function onSubmit(event) {
    event.preventDefault()
    setError('')
    try {
      await api.createFeatured({
        kind: form.kind,
        placement: 'hero',
        position: Number(form.position),
        resource_slug: form.resource_slug.trim(),
        starts_at: fromUtcInput(form.starts_at),
        ends_at: fromUtcInput(form.ends_at),
      })
      setForm(EMPTY)
      await reload()
    } catch (err) {
      const overlap = (err?.status === 409 || err?.status === 400) && err.fieldErrors?.some((item) => item.field === 'starts_at')
      setError(overlap ? '这个位置在该时间段已经有推荐' : (err?.status === 403 ? '请刷新页面后再试' : (err?.message || '保存失败')))
    }
  }

  return (
    <section className="admin-block glass">
      <h1>推荐位</h1>
      {error ? <p role="alert">{error}</p> : null}
      <table className="admin-table">
        <thead>
          <tr>
            <th>板块</th>
            <th>位置</th>
            <th>资源 slug</th>
            <th>开始</th>
            <th>结束</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id}>
              <td>{KIND_LABEL[item.kind] || item.kind}</td>
              <td>{item.position}</td>
              <td>{item.resource_slug}</td>
              <td>{item.starts_at}</td>
              <td>{item.ends_at || '不结束'}</td>
              <td><button type="button" className="pill" onClick={() => api.deleteFeatured(item.id).then(reload)}>停用</button></td>
            </tr>
          ))}
        </tbody>
      </table>
      <form className="admin-form" onSubmit={onSubmit}>
        <label className="field">板块
          <select value={form.kind} onChange={(event) => setForm({ ...form, kind: event.target.value })}>
            {Object.entries(KIND_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <label className="field">位置
          <input type="number" min="1" value={form.position} onChange={(event) => setForm({ ...form, position: event.target.value })} required />
        </label>
        <label className="field">资源 slug
          <input value={form.resource_slug} onChange={(event) => setForm({ ...form, resource_slug: event.target.value })} required />
        </label>
        <label className="field">开始时间（UTC）
          <input type="datetime-local" value={form.starts_at} onChange={(event) => setForm({ ...form, starts_at: event.target.value })} required />
        </label>
        <label className="field">结束时间（UTC）
          <input type="datetime-local" value={form.ends_at} onChange={(event) => setForm({ ...form, ends_at: event.target.value })} />
        </label>
        <button type="submit" className="pill">添加推荐</button>
      </form>
    </section>
  )
}
