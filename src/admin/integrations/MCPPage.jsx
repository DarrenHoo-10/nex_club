import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useAdminSession } from '../session.jsx'
import ResourcePreview from '../ResourcePreview.jsx'
import MCPSettings from './MCPSettings.jsx'
const OPERATIONS = { create_draft: '创建草稿', save_draft: '修改草稿', publish: '发布内容', hide: '下架内容', show: '恢复展示', run_source: '运行一次采集', retry_processing: '重试加工' }
export default function MCPPage() {
  const { api } = useAdminSession()
  const [params, setParams] = useSearchParams()
  const [tab, setTab] = useState(params.has('action') ? 'actions' : 'connections')
  const filterId = params.get('action')
  const [actions, setActions] = useState([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [preview, setPreview] = useState(null)
  const [reviewed, setReviewed] = useState({})
  const [version, setVersion] = useState(0)
  useEffect(() => { let alive = true; (filterId ? api.mcpAction(filterId).then(a => ({ items: [a] })) : api.mcpActions()).then(d => { if (alive) setActions(d.items) }).catch(e => { if (alive) setError(e.message) }); return () => { alive = false } }, [api, version, filterId])
  async function confirm(action, reject) {
    setBusy(true); setError('')
    try { await api.mcpConfirm(action.id, { reject, preview_digest: action.preview_digest }); setVersion(v => v + 1) } catch (e) { setError(e.message) } finally { setBusy(false) }
  }
  return <>
    <div className="auto-section-title"><div><h2>连接你的 AI 工作流</h2><p>在 Codex 中讨论素材，通过 Nex Club 插件管理内容。</p></div><button className="pill" onClick={() => setVersion(v => v + 1)}>刷新</button></div>
    <div className="auto-tabs"><button className={`pill ${tab === 'connections' ? 'on' : ''}`} onClick={() => setTab('connections')}>连接管理</button><button className={`pill ${tab === 'actions' ? 'on' : ''}`} onClick={() => setTab('actions')}>操作预览与记录</button></div>
    {error && <p className="auto-alert" role="alert">{error}</p>}
    {filterId && tab === 'actions' && <p><button className="pill" onClick={() => setParams({})}>显示全部操作</button></p>}
    {tab === 'connections' ? <MCPSettings api={api} /> : <div className="mcp-actions">{actions.length ? actions.filter(a => !filterId || a.id === filterId).map(action => {
      const resource = action.preview?.kind ? action.preview : null
      const expired = new Date(action.expires_at) < new Date()
      return <article className="mcp-action glass" key={action.id}>
        <div className="auto-section-title"><strong>{OPERATIONS[action.operation]}</strong><span className="hint">{expired && action.status === 'pending' ? '已过期' : ({ pending: '待确认', completed: '已执行', rejected: '已取消' })[action.status]}</span></div>
        <h3>{resource?.title || action.preview?.description || action.operation}</h3>
        {resource && <p>{resource.summary}</p>}
        {action.status === 'pending' && ['publish', 'show'].includes(action.operation) && <p>确认后会公开展示内容。</p>}
        {action.status === 'pending' && action.operation === 'hide' && <p>确认后会从公开页面移除。</p>}
        {['create_draft', 'save_draft'].includes(action.operation) && <p className="hint">仅保存草稿，后续单独确认发布。</p>}
        <details><summary>查看完整操作内容</summary><pre>{JSON.stringify(action.arguments, null, 2)}</pre></details>
        <div className="row-actions">
          {resource && <button className="pill" onClick={() => { setPreview(resource); setReviewed(v => ({ ...v, [action.id]: action.preview_digest })) }}>预览效果</button>}
          {action.status === 'pending' && !expired && <><button className="pill primary" disabled={busy || (resource && reviewed[action.id] !== action.preview_digest)} onClick={() => confirm(action, false)}>确认{OPERATIONS[action.operation]}</button><button className="pill" disabled={busy} onClick={() => confirm(action, true)}>取消</button></>}
          {action.result?.id && ['create_draft', 'save_draft', 'publish', 'hide', 'show'].includes(action.operation) && <Link className="pill" to={`/admin/resources/${action.result.id}`}>打开资源 ↗</Link>}
        </div>
        {action.status === 'pending' && resource && reviewed[action.id] !== action.preview_digest && <small className="hint">先预览效果，再确认执行。</small>}
      </article>
    }) : <p className="empty">还没有操作记录。通过插件准备草稿或发布流程后，可以在这里查看。</p>}</div>}
    {preview && <ResourcePreview resource={preview} banner="检查本次操作的展示效果，关闭预览后确认执行。" onClose={() => setPreview(null)} />}
  </>
}
