import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useAdminSession } from '../session.jsx'
import ResourceDialog from '../../components/ResourceDialog.jsx'
import { safeHttpUrl } from '../../api/view.js'
import { date, ErrorNotice, Pager, ReadState, STAGES, Status, STATUS, useAdminRead } from './common.jsx'

export function SourceContext({ data }) {
  const href = safeHttpUrl(data?.source_url)
  return <><p className="hint">来源：{data.source_name}{href && <> · <a className="auto-text-link" href={href} target="_blank" rel="noopener noreferrer">查看原文 ↗</a></>}</p><details className="auto-original"><summary>查看采集的原始资料</summary><pre>{data.body || '未提供正文'}</pre>{data.body_truncated && <p className="hint">正文较长，此处展示前 30,000 字符。</p>}</details></>
}
export default function TasksPage() {
  const { api } = useAdminSession()
  const [params, setParams] = useSearchParams()
  const type = params.get('type') === 'ingest' ? 'ingest' : 'processing'
  const offset = Math.max(0, Number(params.get('offset')) || 0)
  const state = useAdminRead(type === 'ingest' ? 'listIngestRuns' : 'listProcessingRuns', { status: params.get('status') || '', source_id: params.get('source_id') || '', offset }, 10000)
  const [detail, setDetail] = useState(null)
  const [rerun, setRerun] = useState(null)
  const [error, setError] = useState(null)
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (values) => setParams((old) => { const next = new URLSearchParams(old); Object.entries(values).forEach(([key, value]) => value ? next.set(key, value) : next.delete(key)); return next })
  async function retry(id) {
    setBusy(true); setError(null)
    try { await api.retryProcessing(id); setNotice('已按原计划重新入队，不会新建加工轮次'); state.reload() } catch (err) { setError(err) } finally { setBusy(false) }
  }
  async function newRound() { setBusy(true); setError(null); try { await api.rerunProcessing(rerun.id); setNotice('新一轮加工已开始，使用当前模型配置'); setRerun(null); state.reload() } catch (err) { setError(err); setRerun(null) } finally { setBusy(false) } }
  async function inspect(id) { setError(null); try { setDetail(await api.processingContext(id)) } catch (err) { setError(err) } }
  return <>
    <div className="auto-section-title"><div><h2>采集与加工</h2><p>查看执行阶段、原始资料和失败原因。页面每 10 秒更新。</p></div><button className="pill" onClick={state.reload} disabled={state.loading}>刷新</button></div>
    <div className="auto-filters"><div className="row-actions" role="group" aria-label="任务类型"><button className={`pill${type === 'ingest' ? ' on' : ''}`} onClick={() => set({ type: 'ingest', status: '', offset: '' })}>采集记录</button><button className={`pill${type === 'processing' ? ' on' : ''}`} onClick={() => set({ type: 'processing', status: '', offset: '' })}>加工阶段</button></div>
      <label className="field">任务状态<select value={params.get('status') || ''} onChange={(e) => set({ status: e.target.value, offset: '' })}><option value="">全部</option>{(type === 'ingest' ? ['pending', 'running', 'succeeded', 'failed', 'cancelled'] : ['pending', 'running', 'succeeded', 'failed', 'blocked', 'stale']).map((key) => <option key={key} value={key}>{STATUS[key]}</option>)}</select></label>
      {params.get('source_id') && <button className="pill" onClick={() => set({ source_id: '', offset: '' })}>查看全部信源</button>}
    </div><ErrorNotice error={error} />{notice && <p className="auto-notice" role="status">{notice}</p>}
    <section className="auto-panel glass"><ReadState state={state} />
      <div className="auto-table-wrap"><table className="admin-table"><thead><tr><th>{type === 'ingest' ? '来源' : '资料 / 阶段'}</th><th>状态</th><th>时间</th><th>结果与操作</th></tr></thead><tbody>{state.data?.items.map((run) => <tr key={run.id}>
        <td><strong>{run.title || run.source_name}</strong>{type === 'processing' && <small>{run.source_name} · {STAGES[run.stage]} · 第 {run.rerun_no + 1} 轮</small>}</td><td><Status value={run.status} /></td><td><small>{date(run.created_at)}</small></td>
        <td>{run.error_message && <p className="auto-error-text">{run.error_message}</p>}{run.uncertain_call && <p className="hint">有模型调用尚未确认，请先核对费用与结果。</p>}
          <div className="row-actions">{type === 'processing' ? <><button className="pill" onClick={() => inspect(run.id)}>查看过程</button>{['failed', 'blocked'].includes(run.status) && <button className="pill" disabled={busy || run.uncertain_call} onClick={() => retry(run.id)}>重试本阶段</button>}{!['pending', 'running'].includes(run.status) && <button className="pill" disabled={busy || run.uncertain_call} onClick={() => setRerun(run)}>重新加工</button>}{run.proposal_id && <Link className="auto-text-link" to={`/admin/automation/reviews/${run.proposal_id}`}>审核内容 →</Link>}</> : <Link className="auto-text-link" to={`/admin/automation/tasks?source_id=${run.source_id}&type=processing`}>查看加工结果 →</Link>}</div>
        </td></tr>)}</tbody></table></div><Pager data={state.data} offset={offset} setOffset={(n) => set({ offset: String(n) })} />
    </section>
    {rerun && <ResourceDialog title="确认重新加工" eyebrow="新加工轮次" closeLabel="取消" onClose={() => setRerun(null)}><p>将使用最新原始资料与当前模型重新生成建议，可能产生新的模型费用。已有公开内容保持原样。</p><button className="pill on" disabled={busy} onClick={newRound}>确认重新加工</button></ResourceDialog>}
    {detail && <ResourceDialog title={detail.title} eyebrow="加工过程" onClose={() => setDetail(null)}><SourceContext data={detail} /><ol className="auto-stages">{detail.stages.map((stage) => <li key={stage.id}><div><strong>{STAGES[stage.stage]}</strong><Status value={stage.status} /></div>{stage.error_message && <p>{stage.error_message}</p>}<small>{date(stage.created_at)} · 已执行 {stage.attempt_count} 次</small></li>)}</ol></ResourceDialog>}
  </>
}
