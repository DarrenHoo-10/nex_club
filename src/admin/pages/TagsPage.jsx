import { useEffect, useState } from 'react'
import ConfirmDialog from '../ConfirmDialog.jsx'
import { DIMENSIONS } from '../labels.js'
import { useAdminSession } from '../session.jsx'

export default function TagsPage() {
  const { api } = useAdminSession()
  const [tags, setTags] = useState([])
  const [form, setForm] = useState({ name: '', dimension: 'capability', slug: '' })
  const [sourceId, setSourceId] = useState('')
  const [targetId, setTargetId] = useState('')
  const [confirm, setConfirm] = useState(false)
  const [error, setError] = useState('')

  const reload = () => api.listTags().then((body) => setTags(body.tags || [])).catch((err) => setError(err.message || '加载失败'))

  useEffect(() => { reload() }, [api])

  async function onCreate(event) {
    event.preventDefault()
    setError('')
    try {
      await api.createTag(form)
      setForm({ name: '', dimension: form.dimension, slug: '' })
      await reload()
    } catch (err) {
      setError(messageOf(err))
    }
  }

  async function onMerge() {
    setConfirm(false)
    setError('')
    try {
      await api.mergeTag(sourceId, { target_id: targetId })
      setSourceId('')
      setTargetId('')
      await reload()
    } catch (err) {
      setError(messageOf(err))
    }
  }

  return (
    <section className="admin-block glass">
      <h1>标签</h1>
      {error ? <p role="alert">{error}</p> : null}
      <form className="admin-form" onSubmit={onCreate}>
        <label className="field">名称
          <input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} required />
        </label>
        <label className="field">维度
          <select value={form.dimension} onChange={(event) => setForm({ ...form, dimension: event.target.value })}>
            {DIMENSIONS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </label>
        <label className="field">slug
          <input value={form.slug} onChange={(event) => setForm({ ...form, slug: event.target.value })} required />
        </label>
        <button type="submit" className="pill">新建标签</button>
      </form>
      <ul>
        {tags.map((tag) => <li key={tag.id}>{tag.name} · {tag.dimension} · {tag.slug}</li>)}
      </ul>
      <div className="admin-form">
        <label className="field">要合并的标签
          <select value={sourceId} onChange={(event) => setSourceId(event.target.value)}>
            <option value="">选择</option>
            {tags.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
          </select>
        </label>
        <label className="field">合并到
          <select value={targetId} onChange={(event) => setTargetId(event.target.value)}>
            <option value="">选择目标</option>
            {tags.filter((tag) => tag.id !== sourceId).map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
          </select>
        </label>
        <button type="button" className="pill" disabled={!sourceId || !targetId} onClick={() => setConfirm(true)}>合并</button>
      </div>
      <ConfirmDialog open={confirm} title="确认合并标签" confirmLabel="确认合并" onConfirm={onMerge} onClose={() => setConfirm(false)}>
        <p>正式内容上的旧标签会改挂到目标。</p>
      </ConfirmDialog>
    </section>
  )
}

function messageOf(err) {
  if (err?.status === 403) return '请刷新页面后再试'
  return err?.message || '操作失败'
}
