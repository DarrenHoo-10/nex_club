import { Link } from 'react-router-dom'
import { ErrorNotice, useAdminRead } from './common.jsx'

export default function OverviewPage() {
  const state = useAdminRead('automationOverview', {}, 15000)
  const data = state.data
  return <>
    <div className="auto-section-title"><h2>内容更新一览</h2><button className="pill" onClick={state.reload} disabled={state.loading}>刷新</button></div>
    <ErrorNotice error={state.error} />
    {!data ? <p className="empty" role="status">加载中…</p> : <>
      <div className="auto-metrics">
        {[
          ['待审核内容', data.pending_reviews, '/reviews', '确认后才会发布'],
          ['启用的信源', `${data.enabled_sources} / ${data.sources}`, '/sources', '控制内容从哪里来'],
          ['24 小时新资料', data.new_items_24h, '/tasks', '采集并去重后的资料'],
          ['失败或阻塞的加工阶段', data.blocked_stages, '/tasks?type=processing', '查看原因并处理'],
        ].map(([label, value, path, hint]) => <Link className="auto-metric glass" key={label} to={`/admin/automation${path}`}><span>{label}</span><strong>{value}</strong><small>{hint} ↗</small></Link>)}
      </div>
      <section className="auto-panel glass"><div className="auto-section-title"><h2>从来源到发布</h2><span className="hint">新内容须审核后发布</span></div>
        <div className="auto-flow">{[['01', '添加信源', '设置来源和采集频率', 'sources'], ['02', '自动加工', '筛选、整理、评分、写作', 'tasks'], ['03', '审核并发布', '看原文、核对差异、预览', 'reviews']].map(([n, title, text, path]) => <Link to={`/admin/automation/${path}`} key={n}><b>{n}</b><h3>{title}</h3><p>{text}</p><span>进入 →</span></Link>)}</div>
      </section>
      <div className="auto-two-columns">
        <section className="auto-panel glass"><h2>需要关注</h2><div className="auto-summary-row"><span>采集中的任务</span><Link to="/admin/automation/tasks?type=ingest">{data.active_fetches}</Link></div><div className="auto-summary-row"><span>失败的采集记录</span><Link to="/admin/automation/tasks?type=ingest&status=failed">{data.failed_fetches}</Link></div><div className="auto-summary-row"><span>结果待核对的模型调用</span><Link to="/admin/automation/usage?status=unknown">{data.unknown_calls}</Link></div></section>
        <section className="auto-panel glass"><h2>当前加工策略</h2><p>{data.model.mode === 'live' ? `已启用真实模型：${data.model.name}` : data.model.mode === 'fixture' ? '当前使用测试夹具，不会产生真实模型结果。' : '模型调用已关闭，资料加工会等待启用。'}</p><p>新资源与介绍文字由你审核后发布。{data.model.auto_apply_fields ? '已开启可信字段白名单自动更新。' : '结构字段自动采纳尚未开启。'}</p><Link className="auto-text-link" to="/admin/automation/usage">查看调用与费用 →</Link></section>
      </div>
    </>}
  </>
}
