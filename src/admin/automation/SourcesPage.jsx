import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useAdminSession } from '../session.jsx'
import { date, ErrorNotice, Pager, ReadState, SOURCE_KINDS, Status, useAdminRead } from './common.jsx'
import SourceConfigFields, { sourceConfig } from './SourceConfigFields.jsx'
import SourcePresets from './SourcePresets.jsx'
import SourcePreview from './SourcePreview.jsx'

const initial = { name: '', kind: 'github', repository: '', feed_url: '', config: { initial_backfill_limit: 8 }, participation_mode: 'content', trust_tier: 'community', enabled: false, interval_seconds: 3600, allow_fulltext: false, edit_version: 0 }
function sourceBody(item) { return Object.fromEntries(Object.keys(initial).map((key) => [key, item[key] ?? initial[key]])) }
export default function SourcesPage() {
  const { api } = useAdminSession()
  const [offset, setOffset] = useState(0)
  const state = useAdminRead('listSources', { offset }, 20000)
  const caps = useAdminRead('sourceCapabilities')
  const [form, setForm] = useState(null)
  const [editing, setEditing] = useState(null)
  const [busy, setBusy] = useState(false)
  const [testing, setTesting] = useState(false)
  const [error, setError] = useState(null)
  const [notice, setNotice] = useState('')
  const [catalog, setCatalog] = useState(false)
  const [preview, setPreview] = useState(null)
  const generation = useRef(0)
  useEffect(() => () => { generation.current += 1 }, [])
  const capability = (kind) => caps.data?.paid.find((item) => item.kind === kind)
  const isPaid = (kind) => kind === 'x' || kind === 'wechat'
  const canFetch = (kind) => kind !== 'external' && (!isPaid(kind) || capability(kind)?.ready)
  const patch = (value) => { generation.current += 1; setTesting(false); setForm((old) => ({ ...old, ...value })) }
  const patchConfig = (value) => { generation.current += 1; setTesting(false); setForm((old) => ({ ...old, config: { ...old.config, ...value } })) }
  async function act(work, message) {
    setBusy(true); setError(null); setNotice('')
    try { await work(); setNotice(message); state.reload() } catch (err) { setError(err) } finally { setBusy(false) }
  }
  async function save(event) {
    event.preventDefault()
    await act(async () => { await api.saveSource(editing, form); generation.current += 1; setForm(null); setTesting(false) }, '信源配置已保存')
  }
  async function testSource(input) {
    const key = ++generation.current
    setTesting(true); setError(null); setPreview(null)
    try { const result = await api.previewSource(input); if (generation.current === key) setPreview({ result, name: input.name }) } catch (err) { if (generation.current === key) setError(err) } finally { if (generation.current === key) setTesting(false) }
  }
  function openForm(item) { generation.current += 1; setTesting(false); setEditing(item?.id || null); setForm(item ? sourceBody(item) : { ...initial, config: sourceConfig('github') }); setError(null) }
  function presetPreview(item) {
    const next = { ...initial, ...item, config: { initial_backfill_limit: item.initial_backfill_limit || 8 }, enabled: false }
    delete next.id; delete next.imported; delete next.initial_backfill_limit
    setEditing(null); setForm(next); setCatalog(false); testSource(next)
  }
  const paid = form && capability(form.kind)
  return <>
    <div className="auto-section-title"><div><h2>信源管理</h2><p>先试抓确认标题、原文与日期，再保存信源并启用自动采集。</p></div><div className="row-actions"><button className="pill" onClick={state.reload} disabled={state.loading}>刷新</button><button className="pill" onClick={() => setCatalog(true)}>AIHOT 示范信源</button><button className="pill on" onClick={() => openForm(null)}>添加信源</button></div></div>
    <ErrorNotice error={error || caps.error} />{notice && <p className="auto-notice" role="status">{notice}</p>}
    {testing && <p className="auto-notice" role="status">正在试抓…不会写入资料或启动模型加工。</p>}
    {form && <section className="auto-panel glass"><div className="auto-section-title"><h3>{editing ? '编辑信源' : '添加信源'}</h3><button className="pill" disabled={busy} onClick={() => { generation.current += 1; setTesting(false); setForm(null) }}>取消</button></div>
      <form className="auto-source-form admin-form" onSubmit={save}>
        <label className="field">信源名称<input required maxLength={120} value={form.name} onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className="field">来源类型<select disabled={!!editing} value={form.kind} onChange={(e) => patch({ kind: e.target.value, config: sourceConfig(e.target.value), repository: '', feed_url: '', enabled: false })}>{Object.entries(SOURCE_KINDS).filter(([kind]) => kind !== 'external' || editing).map(([kind, name]) => <option key={kind} value={kind}>{name}</option>)}</select></label>
        {form.kind === 'github' && <label className="field auto-wide">仓库（owner/name）<input required placeholder="例如 KKKKhazix/AIHOT" value={form.repository} onChange={(e) => patch({ repository: e.target.value.trim().replace(/^https:\/\/github\.com\//i, '').replace(/\/$/, '') })} /></label>}
        {form.kind === 'rss' && <label className="field auto-wide">订阅网址<input type="url" required placeholder="https://example.com/feed.xml" value={form.feed_url} onChange={(e) => patch({ feed_url: e.target.value })} /></label>}
        <SourceConfigFields kind={form.kind} config={form.config || {}} onChange={patchConfig} />
        {isPaid(form.kind) && <div className="auto-wide source-provider-state"><strong>{paid?.ready ? '服务商配置已就绪' : '等待服务商配置'}</strong><p>{form.kind === 'x' ? 'X 通过 SocialData 采集。' : '公众号通过极致了（Dajiala）采集。'}密钥只在服务端保存，试抓与正式采集均受预算约束。</p>{!paid?.ready && <p>可以先保存为暂停信源；配置完成后才能试抓或启用。</p>}<details><summary>配置项</summary><ul>{paid?.required_env.map((name) => <li key={name}><code>{name}</code></li>)}</ul></details></div>}
        <label className="field">采集间隔（分钟）<input type="number" min={5} max={10080} required value={form.interval_seconds / 60} onChange={(e) => patch({ interval_seconds: Number(e.target.value) * 60 })} disabled={form.kind === 'external'} /></label>
        <label className="field">首次收录上限<input type="number" min={1} max={100} value={form.config?.initial_backfill_limit || 8} onChange={(e) => patchConfig({ initial_backfill_limit: Number(e.target.value) })} disabled={form.kind === 'external'} /></label>
        <label className="field">资料用途<select value={form.participation_mode} onChange={(e) => patch({ participation_mode: e.target.value })}><option value="content">生成内容建议</option><option value="signal">仅作为线索或指标</option><option value="internal">仅存档，不加工</option></select></label>
        <label className="field">来源可信度<select value={form.trust_tier} onChange={(e) => patch({ trust_tier: e.target.value })}><option value="community">社区来源</option><option value="verified">已核实</option><option value="official">官方来源</option><option value="excluded">排除</option></select></label>
        <div className="auto-source-checks auto-wide"><label><input type="checkbox" checked={form.enabled} disabled={isPaid(form.kind) && !paid?.ready} onChange={(e) => patch({ enabled: e.target.checked })} />启用采集</label><label><input type="checkbox" checked={form.allow_fulltext} onChange={(e) => patch({ allow_fulltext: e.target.checked })} />允许展示来源全文</label></div>
        <p className="hint auto-wide">试抓最多展示 20 条，不保存为资料。保存启用后按频率运行，生成内容建议可能调用模型。</p>
        <div className="auto-wide row-actions"><button type="button" className="pill" disabled={busy || testing || !canFetch(form.kind)} onClick={() => testSource(form)}>{testing ? '试抓中…' : '试抓预览'}</button><button type="submit" className="pill on" disabled={busy || testing}>{busy ? '保存中…' : '保存信源'}</button></div>
      </form>
    </section>}
    <section className="auto-panel glass"><ReadState state={state} empty="还没有信源，可以先导入 AIHOT 的示范清单。" />
      <div className="auto-table-wrap"><table className="admin-table"><thead><tr><th>信源</th><th>状态 / 频率</th><th>最近采集</th><th>资料</th><th>操作</th></tr></thead><tbody>{state.data?.items.map((item) => <tr key={item.id}>
        <td><strong>{item.name}</strong><small>{SOURCE_KINDS[item.kind] || item.kind} · {item.repository || item.feed_url || item.config?.url || item.config?.query || item.config?.ghid || '外部推送'}</small><small>{({ content: '生成内容建议', signal: '线索 / 指标', internal: '仅存档' })[item.participation_mode]}</small>{isPaid(item.kind) && !capability(item.kind)?.ready && <span className="auto-status auto-status-blocked">待配置服务商</span>}</td>
        <td><span className={`auto-status ${item.enabled && item.trust_tier !== 'excluded' ? 'auto-status-succeeded' : ''}`}>{item.trust_tier === 'excluded' ? '已排除' : item.enabled ? '已启用' : '已暂停'}</span><small>{item.kind === 'external' ? '接收推送' : `每 ${item.interval_seconds / 60} 分钟`}</small></td>
        <td>{item.last_run ? <><Status value={item.last_run.status} /><small>{date(item.last_run.created_at)}</small>{item.last_run.error_message && <small className="auto-error-text">{item.last_run.error_message}</small>}</> : <span className="hint">尚无主动采集记录</span>}</td>
        <td>{item.item_count}</td>
        <td><div className="row-actions"><button className="pill" disabled={busy || !SOURCE_KINDS[item.kind]} onClick={() => openForm(item)}>配置</button>
          <button className="pill" disabled={busy || !SOURCE_KINDS[item.kind] || (!item.enabled && isPaid(item.kind) && !capability(item.kind)?.ready)} onClick={() => act(() => api.saveSource(item.id, { ...sourceBody(item), enabled: !item.enabled }), item.enabled ? '信源已暂停，已发布内容保留' : '信源已启用')}>{item.enabled ? '暂停' : '启用'}</button>
          {item.kind !== 'external' && <><button className="pill" disabled={busy || testing || !canFetch(item.kind)} onClick={() => testSource(sourceBody(item))}>试抓</button><button className="pill" disabled={busy || !item.enabled || !canFetch(item.kind) || item.trust_tier === 'excluded' || ['pending', 'running'].includes(item.last_run?.status)} onClick={() => act(() => api.runSource(item.id, { edit_version: item.edit_version }), '采集任务已进入队列，可在「采集与加工」查看进度')}>立即采集</button></>}
          <Link className="auto-text-link" to={`/admin/automation/tasks?type=ingest&source_id=${item.id}`}>任务记录</Link></div></td>
      </tr>)}</tbody></table></div><Pager data={state.data} offset={offset} setOffset={setOffset} />
    </section>
    {catalog && <SourcePresets onClose={() => setCatalog(false)} onPreview={presetPreview} onImported={(result) => { setNotice(`已导入 ${result.created} 个暂停信源，跳过 ${result.existing} 个已有信源`); state.reload() }} />}
    {preview && <SourcePreview {...preview} onClose={() => setPreview(null)} />}
  </>
}
