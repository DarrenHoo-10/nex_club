import { useEffect, useRef, useState } from 'react'
import { Link, useBlocker, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useApi } from '../../api/context.jsx'
import ConfirmDialog from '../ConfirmDialog.jsx'
import ResourcePreview from '../ResourcePreview.jsx'
import { previewFromForm } from '../preview.js'
import DetailsFields, { applyDetailsChange } from '../DetailsFields.jsx'
import { diffPayload, formatValue } from '../diff.js'
import { canSave, contentKey, emptyForm, formFromResource, slugValid, toWriteBody } from '../editorState.js'
import { KIND_LABEL, STATUS_LABEL } from '../labels.js'
import { useAdminSession } from '../session.jsx'

export default function ResourceEditorPage() {
  const { id } = useParams()
  const [params, setParams] = useSearchParams()
  const isNew = !id || id === 'new'
  const requested = params.get('kind')
  const kind = ['tool', 'tutorial', 'repo'].includes(requested) ? requested : 'tool'
  const { api } = useAdminSession()
  const publicClient = useApi()
  const navigate = useNavigate()
  const [form, setForm] = useState(() => emptyForm(kind))
  const [savedKey, setSavedKey] = useState(() => contentKey(emptyForm(kind)))
  const [tags, setTags] = useState([])
  const [revisions, setRevisions] = useState([])
  const [fromNo, setFromNo] = useState('')
  const [toNo, setToNo] = useState('')
  const [loading, setLoading] = useState(!isNew)
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState('')
  const [conflict, setConflict] = useState(false)
  const [fieldErrors, setFieldErrors] = useState([])
  const [preview, setPreview] = useState(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [banner, setBanner] = useState('')
  const [confirm, setConfirm] = useState(null)
  const [reason, setReason] = useState('')
  const formRef = useRef(form)
  const savedRef = useRef(savedKey)
  const allowLeave = useRef(false)
  const previewRequest = useRef(0)
  formRef.current = form
  savedRef.current = savedKey

  useEffect(() => {
    allowLeave.current = false
    previewRequest.current += 1
    setPreview(null)
    setPreviewLoading(false)
    return () => { previewRequest.current += 1 }
  }, [id])

  useEffect(() => {
    let alive = true
    api.listTags().then((body) => { if (alive) setTags(body.tags || []) }).catch(() => { if (alive) setTags([]) })
    return () => { alive = false }
  }, [api])

  useEffect(() => {
    if (isNew) return undefined
    let alive = true
    setLoading(true)
    api.getResource(id)
      .then((resource) => {
        if (!alive) return
        const next = formFromResource(resource)
        setForm(next)
        setSavedKey(contentKey(next))
        setLoading(false)
      })
      .catch((err) => {
        if (!alive || err?.status === 401) return
        setNotice(err?.message || '加载失败')
        setLoading(false)
      })
    return () => { alive = false }
  }, [api, id, isNew])

  useEffect(() => {
    if (isNew) return undefined
    let alive = true
    api.listRevisions(id)
      .then((body) => {
        if (!alive) return
        const list = body.revisions || []
        setRevisions(list)
        if (list.length >= 1) {
          setFromNo(String(list[Math.max(0, list.length - 2)].revision_no))
          setToNo(String(list[list.length - 1].revision_no))
        }
      })
      .catch(() => { if (alive) setRevisions([]) })
    return () => { alive = false }
  }, [api, id, isNew, form.editVersion])

  const blocker = useBlocker(() => {
    if (allowLeave.current) return false
    return contentKey(formRef.current) !== savedRef.current
  })

  useEffect(() => {
    if (blocker.state !== 'blocked') return undefined
    let ok = false
    try { ok = window.confirm('有未保存的修改，确定离开？') } catch { ok = false }
    if (ok) blocker.proceed()
    else blocker.reset()
    return undefined
  }, [blocker])

  useEffect(() => {
    const onLeave = (event) => {
      if (contentKey(formRef.current) === savedRef.current) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onLeave)
    return () => window.removeEventListener('beforeunload', onLeave)
  }, [])

  function patch(partial) {
    setForm((prev) => ({ ...prev, ...partial }))
  }

  function toggleUnlock(path) {
    setForm((prev) => {
      const has = prev.unlockFields.includes(path)
      return {
        ...prev,
        unlockFields: has ? prev.unlockFields.filter((item) => item !== path) : prev.unlockFields.concat(path),
      }
    })
  }

  function handleWriteError(err) {
    if (err?.status === 409 && err.code === 'edit_conflict') {
      setConflict(true)
      setNotice('内容已更新')
      return
    }
    if (err?.status === 403) {
      setNotice('请刷新页面后再试')
      return
    }
    setFieldErrors(err?.fieldErrors || [])
    setNotice(err?.message || '保存失败')
  }

  async function onSave(event) {
    event.preventDefault()
    setSaving(true)
    setNotice('')
    setConflict(false)
    setFieldErrors([])
    try {
      if (isNew) {
        const result = await api.createResource({ kind: form.kind, ...toWriteBody(form, { includeVersion: false }) })
        allowLeave.current = true
        savedRef.current = contentKey(formRef.current)
        navigate(`/admin/resources/${result.id}`, { replace: true })
        return
      }
      const result = await api.saveResource(id, toWriteBody(form))
      const next = {
        ...form,
        editVersion: result.edit_version ?? form.editVersion,
        revisionId: result.revision_id ?? form.revisionId,
        fieldLocks: result.field_locks ?? form.fieldLocks,
        slug: result.slug || form.slug,
        changeReason: '',
        unlockFields: [],
        hasDraft: true,
      }
      setForm(next)
      setSavedKey(contentKey(next))
      setNotice('草稿已保存')
    } catch (err) {
      handleWriteError(err)
    } finally {
      setSaving(false)
    }
  }

  async function loadServer() {
    const resource = await api.getResource(id)
    const next = formFromResource(resource)
    setForm(next)
    setSavedKey(contentKey(next))
    setConflict(false)
    setNotice('')
  }

  async function onPreview() {
    const request = ++previewRequest.current
    setPreviewLoading(true)
    setNotice('')
    try {
      if (isNew || contentKey(formRef.current) !== savedRef.current) {
        setPreview(previewFromForm(formRef.current, tags, id))
        setBanner('当前编辑内容 · 尚未保存')
      } else {
        const current = formRef.current
        const result = await api.preview(id)
        if (request !== previewRequest.current) return
        setPreview(result)
        setBanner(current.hasDraft ? '已保存草稿 · 未发布' : current.status === 'published' ? '当前已发布版本' : '已保存内容 · 公开页不可见')
      }
    } catch (err) {
      if (request === previewRequest.current) setNotice(err?.message || '预览失败')
    } finally {
      if (request === previewRequest.current) setPreviewLoading(false)
    }
  }

  async function onPublish() {
    setConfirm(null)
    setSaving(true)
    setNotice('')
    try {
      const result = await api.publish(id, { edit_version: form.editVersion, revision_id: form.revisionId })
      const next = {
        ...formRef.current,
        editVersion: result.edit_version ?? form.editVersion,
        revisionId: result.revision_id ?? form.revisionId,
        firstPublishedAt: result.first_published_at || new Date().toISOString(),
        status: 'published',
        changeReason: '',
        unlockFields: [],
        hasDraft: false,
      }
      setForm(next)
      setSavedKey(contentKey(next))
      savedRef.current = contentKey(next)
      try {
        await publicClient.getBySlug(next.slug)
        allowLeave.current = true
        navigate(`/resources/${next.slug}`)
      } catch (err) {
        if (err?.status === 404 || err?.code === 'not_found') {
          setPreview(await api.preview(id))
          setBanner('公开页当前不可见，以下为预览')
          return
        }
        allowLeave.current = true
        navigate(`/resources/${next.slug}`)
      }
    } catch (err) {
      handleWriteError(err)
    } finally {
      setSaving(false)
    }
  }

  async function onVisibility() {
    const action = confirm
    setConfirm(null)
    setSaving(true)
    try {
      const result = await api.setVisibility(id, {
        edit_version: form.editVersion,
        status: action.status,
        reason: reason.trim() || '管理员操作',
      })
      const next = { ...form, editVersion: result.edit_version ?? form.editVersion, status: result.status || action.status }
      setForm(next)
      // Visibility does not persist content. Preserve the last saved baseline
      // so unsaved edits still trigger the navigation warning.
      setReason('')
    } catch (err) {
      handleWriteError(err)
    } finally {
      setSaving(false)
    }
  }

  const dirty = contentKey(form) !== savedKey
  const left = revisions.find((item) => String(item.revision_no) === fromNo)
  const right = revisions.find((item) => String(item.revision_no) === toNo)
  const changes = left && right ? diffPayload(left.payload, right.payload) : []
  const categories = tags.filter((tag) => tag.dimension === 'category')

  return (
    <section className="admin-block glass" aria-busy={loading}>
      <div className="row-actions">
        <h1>{isNew ? `新建${KIND_LABEL[form.kind]}` : form.title || '编辑资源'}</h1>
        <button type="button" className="pill admin-preview-button" disabled={loading || saving || previewLoading || !form.title.trim()} onClick={onPreview}>{previewLoading ? '加载预览…' : '预览'}</button>
        <Link className="pill" to="/admin/resources">返回列表</Link>
      </div>
      {notice ? <p role="alert">{notice}</p> : null}
      {conflict ? (
        <div className="row-actions">
          <button type="button" className="pill" onClick={loadServer}>加载服务器版本</button>
        </div>
      ) : null}
      <form className="admin-form" onSubmit={onSave}>
        {isNew ? (
          <label className="field">类型
            <select
              value={form.kind}
              onChange={(event) => {
                const nextKind = event.target.value
                setParams({ kind: nextKind })
                setForm({ ...emptyForm(nextKind), title: form.title, summary: form.summary, slug: form.slug })
              }}
            >
              {Object.entries(KIND_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select>
          </label>
        ) : <p className="hint">{KIND_LABEL[form.kind]} · {STATUS_LABEL[form.status] || form.status} · 版本 {form.editVersion}</p>}
        <label className="field">slug
          <input
            value={form.slug}
            readOnly={Boolean(form.firstPublishedAt)}
            onChange={(event) => patch({ slug: event.target.value })}
          />
        </label>
        {form.slug && !slugValid(form.slug.trim()) ? <p className="hint">slug 需为小写字母、数字和短横线</p> : null}
        {form.firstPublishedAt ? <p className="hint">slug 在发布后保持不变</p> : null}
        <FieldLock form={form} path="title" onToggle={toggleUnlock} />
        <label className="field">标题
          <input value={form.title} onChange={(event) => patch({ title: event.target.value })} />
        </label>
        <FieldLock form={form} path="summary" onToggle={toggleUnlock} />
        <label className="field">简介
          <textarea rows={3} value={form.summary} onChange={(event) => patch({ summary: event.target.value })} />
        </label>
        <label className="field">别名（一行一个）
          <textarea rows={3} value={form.aliasesText} onChange={(event) => patch({ aliasesText: event.target.value })} />
        </label>
        <label className="field">封面 URL（一行一个）
          <textarea rows={3} value={form.coverUrlsText} onChange={(event) => patch({ coverUrlsText: event.target.value })} />
        </label>
        <label className="field">主分类
          <select value={form.primaryCategoryId} onChange={(event) => patch({ primaryCategoryId: event.target.value })}>
            <option value="">无</option>
            {categories.map((tag) => <option key={tag.id} value={tag.id}>{tag.name}</option>)}
          </select>
        </label>
        <fieldset className="field">
          <legend>标签</legend>
          <div className="checks">
            {tags.map((tag) => (
              <label key={tag.id}>
                <input
                  type="checkbox"
                  checked={form.tagIds.includes(tag.id)}
                  onChange={() => patch({
                    tagIds: form.tagIds.includes(tag.id) ? form.tagIds.filter((item) => item !== tag.id) : form.tagIds.concat(tag.id),
                  })}
                />
                {tag.name}
              </label>
            ))}
          </div>
        </fieldset>
        <label className="field">质量分
          <input type="number" min="0" max="100" value={form.qualityScore} onChange={(event) => patch({ qualityScore: Number(event.target.value) })} />
        </label>
        <label className="field">推荐理由
          <textarea rows={3} value={form.recommendationReason} onChange={(event) => patch({ recommendationReason: event.target.value })} />
        </label>
        <DetailsFields form={form} onDetails={(partial) => setForm((prev) => applyDetailsChange(prev, partial))} />
        <label className="field">变更原因
          <input value={form.changeReason} onChange={(event) => patch({ changeReason: event.target.value })} />
        </label>
        {fieldErrors.length > 0 && (
          <ul>
            {fieldErrors.map((item) => <li key={`${item.field}-${item.code}`}>{item.field}：{item.code}</li>)}
          </ul>
        )}
        <div className="row-actions">
          <button type="submit" className="pill" disabled={!canSave(form) || saving}>保存</button>
          {!isNew && (
            <button type="button" className="pill" disabled={dirty || saving || !form.hasDraft} onClick={() => setConfirm({ type: 'publish' })}>
              发布
            </button>
          )}
          {!isNew && visibilityActions(form.status).map((action) => (
            <button key={action.status} type="button" className="pill" disabled={saving} onClick={() => setConfirm(action)}>{action.label}</button>
          ))}
        </div>
      </form>
      {preview && <ResourcePreview resource={preview} banner={banner} onClose={() => setPreview(null)} />}
      {!isNew && revisions.length > 0 && (
        <div className="admin-form">
          <h2>修订</h2>
          <div className="row-actions">
            <label className="field">从
              <select value={fromNo} onChange={(event) => setFromNo(event.target.value)}>
                {revisions.map((item) => <option key={item.id} value={item.revision_no}>{item.revision_no}</option>)}
              </select>
            </label>
            <label className="field">到
              <select value={toNo} onChange={(event) => setToNo(event.target.value)}>
                {revisions.map((item) => <option key={`to-${item.id}`} value={item.revision_no}>{item.revision_no}</option>)}
              </select>
            </label>
          </div>
          <div className="diff-list">
            {changes.length === 0 ? <p className="hint">这两个版本没有字段差异。</p> : changes.map((change) => (
              <div key={change.field}>
                <strong>{change.field}</strong>
                <pre>{formatValue(change.from)}</pre>
                <pre>{formatValue(change.to)}</pre>
              </div>
            ))}
          </div>
        </div>
      )}
      <ConfirmDialog
        open={confirm?.type === 'publish'}
        title="确认发布"
        confirmLabel="确认发布"
        onConfirm={onPublish}
        onClose={() => setConfirm(null)}
      >
        <p>公开页将显示当前已保存的草稿。</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={Boolean(confirm && confirm.type !== 'publish')}
        title={`确认${confirm?.label || ''}`}
        confirmLabel={`确认${confirm?.label || ''}`}
        onConfirm={onVisibility}
        onClose={() => setConfirm(null)}
      >
        <p>{confirm?.body}</p>
        <label className="field">原因
          <input value={reason} onChange={(event) => setReason(event.target.value)} />
        </label>
      </ConfirmDialog>
    </section>
  )
}

function FieldLock({ form, path, onToggle }) {
  if (!form.fieldLocks?.includes(path)) return null
  return (
    <label className="lock-line">
      <span aria-hidden>🔒</span>
      <input type="checkbox" checked={form.unlockFields.includes(path)} onChange={() => onToggle(path)} />
      允许流水线以后改这个字段
    </label>
  )
}

function visibilityActions(status) {
  if (status === 'published') {
    return [
      { status: 'hidden', label: '隐藏', body: '公开页将无法打开这份内容。' },
      { status: 'archived', label: '归档', body: '归档后需要再恢复才会公开。' },
    ]
  }
  if (status === 'hidden') {
    return [
      { status: 'published', label: '恢复公开', body: '这份内容会重新出现在公开页。' },
      { status: 'archived', label: '归档', body: '归档后需要再恢复才会公开。' },
    ]
  }
  if (status === 'archived') return [{ status: 'published', label: '恢复公开', body: '这份内容会重新出现在公开页。' }]
  if (status === 'draft') return [{ status: 'archived', label: '归档', body: '草稿会归档，不再从列表里当草稿发布。' }]
  return []
}
