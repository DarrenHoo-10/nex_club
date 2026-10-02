import { useEffect, useState } from 'react'
export default function MCPSettings({ api }) {
  const [tokens, setTokens] = useState([])
  const [name, setName] = useState('我的 AI 客户端')
  const [prepare, setPrepare] = useState(false)
  const [execute, setExecute] = useState(false)
  const [secret, setSecret] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [version, setVersion] = useState(0)
  useEffect(() => { let alive = true; api.mcpTokens().then(d => { if (alive) setTokens(d.items) }).catch(e => { if (alive) setError(e.message) }); return () => { alive = false } }, [api, version])
  async function create(e) { e.preventDefault(); setBusy(true); setError(''); setSecret(''); try { const d = await api.mcpNewToken({ name, can_prepare: prepare, can_execute: execute }); setSecret(d.token); setVersion(v => v + 1) } catch (e) { setError(e.message) } finally { setBusy(false) } }
  async function revoke(id) { setBusy(true); setError(''); try { await api.mcpRevokeToken(id); setSecret(''); setVersion(v => v + 1) } catch (e) { setError(e.message) } finally { setBusy(false) } }
  return <div className="assistant-mcp glass auto-panel"><h2>把你的 AI 客户端连到内容库</h2><p>使用支持 Streamable HTTP 和 Bearer 令牌的 MCP 客户端。默认只读；可分别授权准备与执行工作流。让客户端加载内容管理 Skill，讨论、预览和确认都在你现有的 AI 对话里完成。</p><label className="field">服务地址<input readOnly value={`${window.location.origin}/mcp`} /></label><p className="hint">客户端填写 Authorization: Bearer &lt;访问令牌&gt;。暂不提供 OAuth 登录；仅支持预先配置令牌的客户端。</p>
    {error && <p className="auto-alert" role="alert">{error}</p>}
    <form onSubmit={create} className="assistant-token-form"><label className="field">连接名称<input value={name} maxLength={80} required onChange={e => setName(e.target.value)} /></label><label><input type="checkbox" checked={prepare} onChange={e => { setPrepare(e.target.checked); if (!e.target.checked) setExecute(false) }} /> 允许准备草稿、发布、采集等操作</label><label><input type="checkbox" checked={execute} disabled={!prepare} onChange={e => setExecute(e.target.checked)} /> 允许客户端在你确认后执行操作（包括发布与下架）</label><button className="pill primary" disabled={busy}>创建访问令牌</button></form>
    {secret && <div className="assistant-secret"><strong>令牌只显示这一次，请保存到客户端配置中。</strong><label className="field">访问令牌<input readOnly value={secret} onFocus={e => e.target.select()} /></label><small>有效期 30 天。不要粘贴到公开页面或聊天素材中。</small><button className="pill" onClick={() => setSecret('')}>已保存，收起</button></div>}
    <h3>已有连接</h3>{tokens.length ? tokens.map(t => <div className="assistant-token" key={t.id}><div><strong>{t.name}</strong><p>{t.can_execute ? '读取 + 准备 + 执行' : t.can_prepare ? '读取 + 准备操作' : '只读'} · {t.revoked_at ? '已撤销' : `有效至 ${new Date(t.expires_at).toLocaleDateString()}`}</p></div>{!t.revoked_at && <button className="pill" disabled={busy} onClick={() => revoke(t.id)}>撤销</button>}</div>) : <p className="hint">还没有外部连接。</p>}
  </div>
}
