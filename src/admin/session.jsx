import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom'

const SessionContext = createContext(null)

export function AdminSessionProvider({ api, children }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [admin, setAdmin] = useState(null)
  const [ready, setReady] = useState(false)

  const onUnauthorized = useCallback(() => {
    setAdmin(null)
    if (location.pathname === '/admin/login') return
    const next = `${location.pathname}${location.search}`
    navigate(`/admin/login?next=${encodeURIComponent(next)}`, { replace: true })
  }, [location.pathname, location.search, navigate])

  const bound = useMemo(() => wrapUnauthorized(api, onUnauthorized), [api, onUnauthorized])

  useEffect(() => {
    let alive = true
    api.current()
      .then((user) => {
        if (!alive) return
        setAdmin(user)
        setReady(true)
      })
      .catch(() => {
        if (!alive) return
        setAdmin(null)
        setReady(true)
      })
    return () => { alive = false }
  }, [api])

  const login = useCallback(async (username, password) => {
    const user = await api.login(username, password)
    setAdmin(user)
    return user
  }, [api])

  const logout = useCallback(async () => {
    await bound.logout()
    setAdmin(null)
  }, [bound])

  const value = useMemo(() => ({ admin, ready, api: bound, login, logout }), [admin, ready, bound, login, logout])
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export function useAdminSession() {
  const value = useContext(SessionContext)
  if (!value) throw new Error('useAdminSession requires AdminSessionProvider')
  return value
}

export function RequireAuth() {
  const { admin, ready } = useAdminSession()
  const location = useLocation()
  if (!ready) return <p className="empty" aria-busy="true">加载中…</p>
  if (!admin) {
    const next = `${location.pathname}${location.search}`
    return <Navigate to={`/admin/login?next=${encodeURIComponent(next)}`} replace />
  }
  return <Outlet />
}

export function safeNext(value) {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return '/admin/resources'
  return value
}

function wrapUnauthorized(api, onUnauthorized) {
  return new Proxy(api, {
    get(target, prop, receiver) {
      const value = Reflect.get(target, prop, receiver)
      if (typeof value !== 'function' || prop === 'login' || prop === 'seed' || prop === 'publishedSnapshot') return value
      return async (...args) => {
        try {
          return await value.apply(target, args)
        } catch (err) {
          if (err?.status === 401) onUnauthorized()
          throw err
        }
      }
    },
  })
}
