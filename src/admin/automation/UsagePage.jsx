import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { date, ErrorNotice, Pager, ReadState, Status, STATUS, useAdminRead } from './common.jsx'

export default function UsagePage() {
  const overview = useAdminRead('automationOverview', {}, 20000)
  const [params, setParams] = useSearchParams()
  const [offset, setOffset] = useState(0)
  const state = useAdminRead('automationCalls', { status: params.get('status') || '', offset }, 20000)
  const model = overview.data?.model
  return <>
    <div className="auto-section-title"><div><h2>模型与费用</h2><p>模型与采集费用按配置价表或服务商回执计算，用于预算控制。</p></div><button className="pill" onClick={() => { state.reload(); overview.reload() }} disabled={state.loading}>刷新</button></div>
    <ErrorNotice error={overview.error} />
    {model && <section className="auto-panel glass"><div className="auto-section-title"><h3>{model.name || '模型尚未配置'}</h3><span className="pill">{({ live: '真实调用已启用', fixture: '测试夹具模式', disabled: '调用已关闭' })[model.mode]}</span></div><p className="hint">模型与预算上限由服务端配置；这里展示实际运行情况，不显示密钥。</p>
      {overview.data.budgets.length ? <div className="auto-budget-grid">{overview.data.budgets.map((budget) => <div key={budget.scope_key}><span>{budget.currency} · 当前预算窗口</span><strong>{Number(budget.spent_amount).toFixed(4)} <small>/ {Number(budget.limit_amount).toFixed(4)}</small></strong><p>已结算 · 另有 {Number(budget.reserved_amount).toFixed(4)} 预占</p><progress max={Number(budget.limit_amount) || 1} value={Number(budget.spent_amount) + Number(budget.reserved_amount)} aria-label="预算使用情况" /><small>截止 {date(budget.window_end)}</small></div>)}</div> : <p>当前窗口还没有预算记录。配置的每日额度：{model.daily_limit || '未配置'} {model.currency || ''}</p>}
    </section>}
    <div className="auto-filters"><label className="field">调用状态<select value={params.get('status') || ''} onChange={(e) => { setParams(e.target.value ? { status: e.target.value } : {}); setOffset(0) }}><option value="">全部</option>{['prepared', 'sent', 'succeeded', 'failed', 'unknown'].map((value) => <option key={value} value={value}>{STATUS[value]}</option>)}</select></label></div>
    <section className="auto-panel glass"><ReadState state={state} /><div className="auto-table-wrap"><table className="admin-table"><thead><tr><th>模型 / 采集渠道</th><th>状态</th><th>输入 / 输出 Token</th><th>费用 / 预占</th><th>时间</th></tr></thead><tbody>{state.data?.items.map((call) => <tr key={call.id}><td><strong>{call.model || call.provider_key}</strong><small>{call.assistant_turn_id ? '历史聊天试用' : call.provider_key === 'openai' ? '内容加工' : call.provider_key}</small></td><td><Status value={call.status} />{call.error_code && <small>{call.error_code}</small>}</td><td>{call.input_tokens ?? '—'} / {call.output_tokens ?? '—'}</td><td>{call.actual_cost == null ? '待结算' : Number(call.actual_cost).toFixed(6)} {call.currency}<small>预占 {Number(call.reserved_cost).toFixed(6)}</small></td><td>{date(call.created_at)}</td></tr>)}</tbody></table></div><Pager data={state.data} offset={offset} setOffset={setOffset} /></section>
  </>
}
