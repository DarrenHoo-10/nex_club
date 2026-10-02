import { useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { safeNext, useAdminSession } from '../session.jsx'

export default function LoginPage() {
  const { admin, ready, login } = useAdminSession()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const next = safeNext(params.get('next'))

  if (!ready) return <p className="empty" aria-busy="true">加载中…</p>
  if (admin) return <Navigate to={next} replace />

  async function onSubmit(event) {
    event.preventDefault()
    setError('')
    try {
      await login(username.trim(), password)
      navigate(next, { replace: true })
    } catch (err) {
      setError(err?.status === 401 ? '用户名或口令不正确' : (err?.message || '登录失败'))
    }
  }

  return (
    <form className="admin-block glass admin-form" onSubmit={onSubmit}>
      <h1>管理员登录</h1>
      <label className="field">用户名
        <input name="username" value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="username" />
      </label>
      <label className="field">口令
        <input name="password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" />
      </label>
      {error ? <p role="alert">{error}</p> : null}
      <button type="submit" className="pill">登录</button>
    </form>
  )
}
