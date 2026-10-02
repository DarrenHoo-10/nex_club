import { useEffect, useState } from 'react'
import { Link, NavLink, Outlet } from 'react-router-dom'
import { useAdminSession } from '../session.jsx'

export const STATUS = { pending: '等待处理', running: '正在运行', succeeded: '已完成', failed: '失败', blocked: '需处理', stale: '资料已更新', cancelled: '已取消', applied: '已发布', partially_applied: '部分采纳并发布', rejected: '已拒绝', conflict: '版本冲突', prepared: '已预占', sent: '已发送', unknown: '结果待核对' }
export const STAGES = { extract: '提取正文', prefilter: '相关性筛选', structure: '整理信息', score: '质量评估', write: '撰写介绍', propose: '生成审核建议' }
export const SOURCE_KINDS = { github: 'GitHub 仓库', rss: 'RSS / Atom', json: 'JSON 接口', web: '网页列表', x: 'X / SocialData', wechat: '公众号 / 极致了', external: '外部推送' }
export function date(value) { return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '—' }
export function Status({ value, label }) { return <span className={`auto-status auto-status-${value}`}>{label || STATUS[value] || value || '—'}</span> }
export function ErrorNotice({ error }) { return error ? <p className="auto-alert" role="alert">{error.message || String(error)}</p> : null }
export function useAdminRead(method, params = {}, interval = 0) {
  const { api } = useAdminSession()
  const key = JSON.stringify(params)
  const requestKey = method + ':' + key
  const [result, setResult] = useState({ key: requestKey, data: null, loading: true, error: null })
  const [version, setVersion] = useState(0)
  useEffect(() => {
    let alive = true
    setResult((old) => ({ key: requestKey, data: old.key === requestKey ? old.data : null, loading: true, error: null }))
    Promise.resolve().then(() => api[method](JSON.parse(key))).then((data) => {
      if (alive) setResult({ key: requestKey, data, loading: false, error: null })
    }).catch((error) => { if (alive) setResult((old) => ({ ...old, loading: false, error })) })
    return () => { alive = false }
  }, [api, method, key, version])
  useEffect(() => {
    if (!interval) return undefined
    const timer = setInterval(() => { if (!document.hidden) setVersion((v) => v + 1) }, interval)
    return () => clearInterval(timer)
  }, [interval])
  return { ...(result.key === requestKey ? result : { data: null, loading: true, error: null }), reload: () => setVersion((v) => v + 1) }
}
export function Pager({ data, offset, setOffset }) {
  if (!offset && !data?.has_more) return null
  return <div className="auto-pager"><button className="pill" disabled={!offset} onClick={() => setOffset(Math.max(0, offset - 50))}>上一页</button><span>第 {Math.floor(offset / 50) + 1} 页</span><button className="pill" disabled={!data?.has_more} onClick={() => setOffset(data.next_offset)}>下一页</button></div>
}
export function ReadState({ state, empty = '暂无记录' }) {
  return <><ErrorNotice error={state.error} />{!state.data && state.loading && <p role="status" className="empty">加载中…</p>}{state.data?.items?.length === 0 && !state.error && <p className="empty">{empty}</p>}</>
}
export function AutomationLayout() {
  return <section className="automation">
    <div className="auto-heading"><div><p className="eyebrow">NEX CLUB / AUTOMATION</p><h1>自动化中心</h1><p>管理信源，跟进加工，在发布前确认内容。</p></div><Link className="pill" to="/admin/resources">查看内容库 ↗</Link></div>
    <nav className="auto-tabs" aria-label="自动化管理">
      {[['', '运行概览'], ['/sources', '信源管理'], ['/tasks', '采集与加工'], ['/reviews', '待审核内容'], ['/usage', '模型与费用'], ['/mcp', 'MCP 接入']].map(([path, name]) => <NavLink key={path} end={!path} className={({ isActive }) => `pill${isActive ? ' on' : ''}`} to={`/admin/automation${path}`}>{name}</NavLink>)}
    </nav><Outlet />
  </section>
}
