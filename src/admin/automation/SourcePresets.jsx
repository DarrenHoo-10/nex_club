import { useState } from 'react'
import ResourceDialog from '../../components/ResourceDialog.jsx'
import { useAdminSession } from '../session.jsx'
import { ErrorNotice, useAdminRead } from './common.jsx'

export default function SourcePresets({ onClose, onImported, onPreview }) {
  const { api } = useAdminSession()
  const state = useAdminRead('sourcePresets')
  const [selected, setSelected] = useState([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const available = state.data?.items.filter((item) => !item.imported) || []
  async function importSelected() {
    setBusy(true); setError(null)
    try { const result = await api.importSourcePresets(selected); setSelected([]); state.reload(); onImported(result) } catch (err) { setError(err) } finally { setBusy(false) }
  }
  return <ResourceDialog title="AIHOT 示范信源" eyebrow="公开 RSS 信源库" closeLabel="返回信源" onClose={onClose}>
    <p className="resource-modal-summary">可选择导入，也可以先试抓。导入后默认暂停、首次最多收录 8 条；已有信源不会被覆盖。</p>
    <ErrorNotice error={error || state.error} />
    {!state.data ? <p role="status">加载示范清单…</p> : <>
      <div className="row-actions source-preset-actions"><button className="pill" disabled={busy || state.loading} onClick={() => setSelected(available.map((item) => item.id))}>全选未导入</button><button className="pill" disabled={busy} onClick={() => setSelected([])}>清空选择</button><button className="pill on" disabled={busy || !selected.length} onClick={importSelected}>{busy ? '导入中…' : `导入所选 ${selected.length} 个`}</button></div>
      <div className="source-preset-list">{state.data.items.map((item) => <div key={item.id} className="source-preset-row"><label><input type="checkbox" aria-label={item.name} disabled={item.imported || busy} checked={selected.includes(item.id)} onChange={(e) => setSelected((old) => e.target.checked ? [...old, item.id] : old.filter((id) => id !== item.id))} /><span><strong>{item.name}</strong><small>{item.feed_url}</small><small>每 {item.interval_seconds / 60} 分钟 · {item.trust_tier === 'official' ? '官方来源' : '社区 / 媒体'}</small></span></label><div className="row-actions">{item.imported && <span className="auto-status auto-status-succeeded">已导入</span>}<button className="pill" disabled={busy} onClick={() => onPreview(item)}>试抓此源</button></div></div>)}</div>
      <p className="hint source-preset-credit">清单来源：<a className="auto-text-link" href={state.data.upstream_url} target="_blank" rel="noopener noreferrer">AIHOT 开源配置 ↗</a> · 已固定版本，不执行上游脚本。</p>
    </>}
  </ResourceDialog>
}
