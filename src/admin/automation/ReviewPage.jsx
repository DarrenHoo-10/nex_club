import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useAdminSession } from '../session.jsx'
import ResourcePreview from '../ResourcePreview.jsx'
import ResourceDialog from '../../components/ResourceDialog.jsx'
import { LEVELS, PRICING, DEPLOYMENTS, PLATFORMS, KIND_LABEL } from '../labels.js'
import { SourceContext } from './TasksPage.jsx'
import { ErrorNotice, Status } from './common.jsx'

const FIELDS = { title: '标题', summary: '简介', body_markdown: '详细正文', aliases: '别名', cover_urls: '封面网址', primary_category_id: '主分类', tag_ids: '标签', quality_score: '质量分', recommendation_reason: '推荐理由', 'details.website_url': '官网网址', 'details.pricing': '收费方式', 'details.platforms': '支持平台', 'details.deployment': '部署方式', 'details.level': '教程难度', 'details.minutes': '预计阅读分钟', 'details.steps': '教程步骤', 'details.author': '作者', 'details.source_url': '原文网址', 'details.notes': '注意事项', 'details.github_repository_id': 'GitHub 仓库编号', 'details.full_name': '仓库名称', 'details.language': '编程语言', 'details.license': '许可证', 'details.archived': '仓库已归档', 'details.last_activity_at': '最近活动时间' }
const ENUMS = { 'details.pricing': PRICING, 'details.level': LEVELS, 'details.platforms': PLATFORMS, 'details.deployment': DEPLOYMENTS }
function show(value, path, tags) {
  if (value == null || value === '') return '未提供'
  if (path === 'tag_ids') return value.map((id) => tags.find((t) => t.id === id)?.name || '原标签').join('、') || '无'
  if (path === 'primary_category_id') return tags.find((t) => t.id === value)?.name || '无'
  if (ENUMS[path]) { const label = (v) => ENUMS[path].find(([key]) => key === v)?.[1] || v; return Array.isArray(value) ? value.map(label).join('、') || '无' : label(value) }
  if (typeof value === 'boolean') return value ? '是' : '否'
  return Array.isArray(value) ? value.join('\n') || '无' : String(value)
}
function Rewrite({ path, value, onChange, tags }) {
  const name = `改写${FIELDS[path] || path}`
  if (path === 'tag_ids' || path === 'details.platforms' || path === 'details.deployment') {
    const options = path === 'tag_ids' ? tags.filter((t) => t.status === 'active').map((t) => [t.id, t.name]) : ENUMS[path]
    return <div className="checks" role="group" aria-label={name}>{options.map(([id, title]) => <label key={id}><input type="checkbox" checked={(value || []).includes(id)} onChange={(e) => onChange(e.target.checked ? [...(value || []), id] : (value || []).filter((v) => v !== id))} />{title}</label>)}</div>
  }
  if (path === 'primary_category_id') return <select aria-label={name} value={value || ''} onChange={(e) => onChange(e.target.value || null)}><option value="">无</option>{tags.filter((t) => t.dimension === 'category' && t.status === 'active').map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}</select>
  if (ENUMS[path]) return <select aria-label={name} value={value || 'unknown'} onChange={(e) => onChange(e.target.value)}>{ENUMS[path].map(([id, title]) => <option key={id} value={id}>{title}</option>)}</select>
  if (path === 'details.archived') return <select aria-label={name} value={value == null ? '' : String(value)} onChange={(e) => onChange(e.target.value === '' ? null : e.target.value === 'true')}><option value="">未知</option><option value="true">是</option><option value="false">否</option></select>
  if (typeof value === 'number') return <input aria-label={name} type="number" min="0" value={value} onChange={(e) => onChange(Number(e.target.value))} />
  const array = ['aliases', 'cover_urls', 'details.steps'].includes(path)
  return <label className="field">{array ? '每行填写一项' : '改写内容'}<textarea aria-label={name} rows={path === 'body_markdown' ? 9 : 3} value={array ? (value || []).join('\n') : value || ''} onChange={(e) => onChange(array ? e.target.value.split('\n').map((v) => v.trim()).filter(Boolean) : e.target.value)} /></label>
}

export default function ReviewPage() {
  const { id } = useParams()
  const { api } = useAdminSession()
  const [proposal, setProposal] = useState(null)
  const [context, setContext] = useState(null)
  const [tags, setTags] = useState([])
  const [fields, setFields] = useState({})
  const [rewrites, setRewrites] = useState({})
  const [unlock, setUnlock] = useState([])
  const [reason, setReason] = useState('')
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(false)
  const [preview, setPreview] = useState(null)
  const [previewKey, setPreviewKey] = useState('')
  const [confirm, setConfirm] = useState(null)
  const [result, setResult] = useState(null)
  const [reload, setReload] = useState(0)
  const operation = useRef(0)
  useEffect(() => {
    let alive = true
    operation.current += 1
    setBusy(false)
    setProposal(null); setError(null); setContext(null); setResult(null); setPreview(null); setPreviewKey(''); setReason(''); setUnlock([])
    Promise.all([api.getProposal(id), api.listTags()]).then(async ([p, t]) => {
      if (!alive) return
      const source = await api.processingContext(p.processing_run_id)
      if (alive) {
        setProposal(p); setTags(t.tags || [])
        setRewrites(Object.fromEntries(Object.entries(p.field_changes).map(([path, change]) => [path, change.new])))
        setContext(source)
        setFields(Object.fromEntries(Object.entries(p.field_changes).map(([path, change]) => [path,
          p.status !== 'pending' ? p.review_decisions?.fields?.[path] || 'reject'
            : change.locked || (path === 'body_markdown' && !source.allow_fulltext) ? 'reject' : 'accept',
        ])))
      }
    }).catch((err) => { if (alive) setError(err) })
    return () => { alive = false; operation.current += 1 }
  }, [api, id, reload])
  const decision = { edit_version: proposal?.base_edit_version ?? 0, fields, rewrites: Object.fromEntries(Object.entries(rewrites).filter(([path]) => fields[path] === 'rewrite')), unlock, reason }
  const currentKey = JSON.stringify(decision)
  const pending = proposal?.status === 'pending' && !!context && !result
  const accepted = Object.values(fields).filter((value) => value !== 'reject').length
  async function openPreview() {
    const request = operation.current
    setBusy(true); setError(null)
    try { const result = await api.previewProposal(id, decision); if (request !== operation.current) return; setPreview(result); setPreviewKey(currentKey) } catch (err) { if (request === operation.current) setError(err) } finally { if (request === operation.current) setBusy(false) }
  }
  async function submit() {
    const request = operation.current
    const rejecting = confirm === 'reject'
    setConfirm(null); setBusy(true); setError(null)
    try {
      const body = rejecting ? { ...decision, fields: Object.fromEntries(Object.keys(fields).map((path) => [path, 'reject'])), rewrites: {}, unlock: [] } : decision
      const response = await api.decideProposal(id, body)
      if (request !== operation.current) return
      setFields(body.fields); setUnlock(body.unlock); setResult(response); setProposal((old) => ({ ...old, status: response.status }))
    } catch (err) { if (request !== operation.current) return; setError(err); if (err.status === 409) setProposal((old) => ({ ...old, status: 'conflict' })) } finally { if (request === operation.current) setBusy(false) }
  }
  const appliedId = result?.resource_id || proposal?.review_decisions?.result_body?.resource_id
  return <>
    <div className="auto-section-title"><Link className="auto-text-link" to="/admin/automation/reviews">← 返回审核列表</Link><button className="pill" disabled={busy} onClick={() => setReload((v) => v + 1)}>重新加载</button></div>
    <ErrorNotice error={error} />
    {!proposal ? <p className="empty" role="status">{error ? '无法读取审核建议' : '加载审核内容…'}</p> : <>
      <section className="auto-panel glass"><div className="auto-section-title"><div><p className="eyebrow">{KIND_LABEL[proposal.proposed_kind]} · {proposal.resource_id ? '已有资源的更新建议' : '新资源候选'}</p><h2>{proposal.proposed_payload?.title || '内容建议'}</h2></div><Status value={proposal.status} label={proposal.status === 'pending' ? '待审核' : undefined} /></div>
        {context && <SourceContext data={context} />}{context && !context.allow_fulltext && proposal.field_changes.body_markdown && <p className="hint">信源未开启全文展示，正文默认不采用；可以人工改写为自己的介绍。</p>}
        {proposal.proposed_payload?.manual_only && <p className="auto-alert">模型对资料相关性不确定，请重点核对原文。</p>}
        {appliedId && <Link className="auto-text-link" to={`/admin/resources/${appliedId}`}>查看已发布资源 →</Link>}
        {proposal.status === 'conflict' && <p className="auto-alert">资料或资源发生了变化。请处理已有草稿或查看最新加工结果，再重新审核。<Link to="/admin/automation/tasks">查看任务 →</Link></p>}
      </section>
      <section className="auto-panel glass"><div className="auto-section-title"><h2>逐项核对变更</h2>{pending && <div className="row-actions"><button className="pill" disabled={busy} onClick={() => setFields(Object.fromEntries(Object.entries(proposal.field_changes).map(([path, change]) => [path, (change.locked && !unlock.includes(path)) || (path === 'body_markdown' && !context?.allow_fulltext) ? 'reject' : 'accept'])))}>接受未保护字段</button><button className="pill" disabled={busy} onClick={() => setFields(Object.fromEntries(Object.keys(fields).map((path) => [path, 'reject'])))}>全部保留原值</button></div>}</div>
        <fieldset className="auto-decisions" disabled={busy || !pending}>{Object.entries(proposal.field_changes).map(([path, change]) => <article className="auto-change" key={path}>
          <div className="auto-section-title"><h3>{FIELDS[path] || path}{change.locked && <span className="auto-lock">已保护</span>}</h3><select aria-label={`${FIELDS[path] || path}处理方式`} value={fields[path] || 'reject'} onChange={(e) => setFields((old) => ({ ...old, [path]: e.target.value }))}><option value="accept" disabled={change.locked && !unlock.includes(path)}>接受建议</option><option value="reject">{proposal.resource_id ? '保留原值' : '不采用'}</option><option value="rewrite" disabled={change.locked && !unlock.includes(path)}>人工改写</option></select></div>
          <div className="auto-diff"><div><span>原内容</span><pre>{show(change.old, path, tags)}</pre></div><div><span>AI 建议</span><pre>{show(change.new, path, tags)}</pre></div></div>
          {change.evidence?.length > 0 && <details className="auto-evidence"><summary>依据原文的 {change.evidence.length} 处证据</summary>{change.evidence.map((e, n) => <blockquote key={n}>{e.excerpt || '详见原始资料'}</blockquote>)}</details>}
          {change.locked && <label className="auto-unlock"><input type="checkbox" checked={unlock.includes(path)} onChange={(e) => { setUnlock((old) => e.target.checked ? [...old, path] : old.filter((v) => v !== path)); if (!e.target.checked) setFields((old) => ({ ...old, [path]: 'reject' })) }} />解除此字段保护，允许本次修改</label>}
          {pending && fields[path] === 'rewrite' && <div className="admin-form auto-rewrite"><Rewrite path={path} value={rewrites[path]} tags={tags} onChange={(value) => setRewrites((old) => ({ ...old, [path]: value }))} /></div>}
        </article>)}</fieldset>
      </section>
      {pending && <section className="auto-panel glass auto-review-actions"><label className="field">审核说明（可选）<input value={reason} disabled={busy} onChange={(e) => setReason(e.target.value)} placeholder="记录采纳或拒绝的原因" /></label><div className="row-actions"><button className="pill" disabled={busy || !accepted} onClick={openPreview}>预览采纳效果</button><button className="pill on" disabled={busy || !accepted || previewKey !== currentKey} onClick={() => setConfirm('publish')}>采纳并发布</button><button className="pill" disabled={busy} onClick={() => setConfirm('reject')}>拒绝整条建议</button><span className="hint">先预览当前选择的 {accepted} 项变更，再确认发布。</span></div></section>}
    </>}
    {preview && <ResourcePreview resource={preview} banner="按当前审核选择生成 · 尚未发布" onClose={() => setPreview(null)} />}
    {confirm && <ResourceDialog title={confirm === 'reject' ? '确认拒绝建议' : '确认采纳并发布'} eyebrow="审核确认" closeLabel="取消" onClose={() => setConfirm(null)}><p>{confirm === 'reject' ? '这条建议将被拒绝，已有公开内容保持原样。' : `将应用当前选择的 ${accepted} 项变更，并立即更新公开内容。`}</p><div className="row-actions"><button className="pill on" disabled={busy} onClick={submit}>{confirm === 'reject' ? '确认拒绝' : '确认发布'}</button></div></ResourceDialog>}
  </>
}
