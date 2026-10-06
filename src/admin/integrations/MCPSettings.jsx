import { useEffect, useState } from 'react'
export default function MCPSettings({ api }) {
  const [tokens, setTokens] = useState([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [version, setVersion] = useState(0)
  useEffect(() => { let alive = true; api.mcpTokens().then(d => { if (alive) setTokens(d.items) }).catch(e => { if (alive) setError(e.message) }); return () => { alive = false } }, [api, version])
  async function revoke(id) { setBusy(true); setError(''); try { await api.mcpRevokeToken(id); setVersion(v => v + 1) } catch (e) { setError(e.message) } finally { setBusy(false) } }
  return <div className="assistant-mcp glass auto-panel"><h2>通过 Nex MCP 网关连接内容库</h2><p>客户端统一使用 Nex MCP 网关令牌，Club 不再创建或接受独立 MCP 令牌。网关通过服务器内部通道访问内容库，目前仅开放查询，不允许准备或执行写入操作。</p><label className="field">网关服务地址<input readOnly value="https://mcp.nexorai.com.cn/mcp" /></label><p className="hint">在客户端配置 Authorization: Bearer &lt;网关访问令牌&gt;。只读仍可读取草稿、隐藏内容及采集素材，请仅向有权访问这些内容的网关用户开放；服务身份与内部连接由运维配置，此页面不代表上游已上线。</p>
    {error && <p className="auto-alert" role="alert">{error}</p>}
    <h3>历史 Club 令牌</h3><p className="hint">以下令牌已不能直连 Club，可继续撤销以清理历史凭据。这里的撤销不会撤销网关令牌。</p>{tokens.length ? tokens.map(t => <div className="assistant-token" key={t.id}><div><strong>{t.name}</strong><p>{t.revoked_at ? '已撤销' : '直连已停用'}</p></div>{!t.revoked_at && <button className="pill" disabled={busy} onClick={() => revoke(t.id)}>撤销</button>}</div>) : <p className="hint">没有历史 Club 令牌。</p>}
  </div>
}
