import { useSearchParams, Link } from 'react-router-dom'
import { KIND_LABEL } from '../labels.js'
import { date, Pager, ReadState, Status, useAdminRead } from './common.jsx'

export default function ReviewsPage() {
  const [params, setParams] = useSearchParams()
  const offset = Math.max(0, Number(params.get('offset')) || 0)
  const status = params.get('status') || 'pending'
  const state = useAdminRead('automationProposals', { status: status === 'all' ? '' : status, kind: params.get('kind') || '', offset }, 15000)
  const set = (key, value) => setParams((old) => { const next = new URLSearchParams(old); value ? next.set(key, value) : next.delete(key); if (key !== 'offset') next.delete('offset'); return next })
  return <>
    <div className="auto-section-title"><div><h2>待审核内容</h2><p>核对来源与变化，预览最终效果，再决定是否发布。</p></div><button className="pill" disabled={state.loading} onClick={state.reload}>刷新</button></div>
    <div className="auto-filters"><label className="field">审核状态<select value={status} onChange={(e) => set('status', e.target.value)}><option value="pending">待审核</option><option value="applied">已采纳</option><option value="partially_applied">部分采纳</option><option value="rejected">已拒绝</option><option value="conflict">版本冲突</option><option value="all">全部</option></select></label><label className="field">资源类型<select value={params.get('kind') || ''} onChange={(e) => set('kind', e.target.value)}><option value="">全部类型</option>{Object.entries(KIND_LABEL).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label></div>
    <section className="auto-panel glass"><ReadState state={state} empty="没有符合条件的审核建议。启用内容信源后，系统会自动生成候选内容。" /><div className="auto-review-list">{state.data?.items.map((item) => <Link className="auto-review-row" key={item.id} to={`/admin/automation/reviews/${item.id}`}><div><span className="auto-review-kind">{KIND_LABEL[item.proposed_kind]} · {item.resource_id ? '更新已有内容' : '新增内容'}</span><h3>{item.title}</h3><p>{item.source_name} · {item.change_count} 项变更 · {date(item.created_at)}</p></div><div className="auto-review-end"><Status value={item.status} label={item.status === 'pending' ? '待审核' : undefined} /><span>查看建议 →</span></div></Link>)}</div><Pager data={state.data} offset={offset} setOffset={(n) => set('offset', String(n))} /></section>
  </>
}
