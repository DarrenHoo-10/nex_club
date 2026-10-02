import { useEffect } from 'react'
import { Link, Outlet, useLocation } from 'react-router-dom'
import Logo from '../components/Logo.jsx'
import { SECTIONS, sectionPath } from '../sections.js'

export default function PublicLayout() {
  const location = useLocation()
  const active = location.pathname.split('/')[1]
  const sort = new URLSearchParams(location.search).get('sort') || ''

  useEffect(() => {
    const onKey = (event) => {
      if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey) return
      const el = document.activeElement
      const tag = el?.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el?.isContentEditable) return
      event.preventDefault()
      document.getElementById('site-search')?.focus()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  return (
    <div className="page">
      <div className="bg" aria-hidden><i className="b1" /><i className="b2" /><i className="b3" /><i className="b4" /></div>
      <header className="nav glass">
        <Link className="logo" to="/tools">
          <Logo />
          <span className="logo-text">Nex <b>Club</b></span>
        </Link>
        <nav className="tabs" aria-label="板块">
          {SECTIONS.map((section) => {
            const on = section.key === active
            const to = on ? `${location.pathname}${location.search}` : sectionPath(section.key, sort)
            return (
              <Link key={section.key} className={on ? 'tab on' : 'tab'} to={to} aria-current={on ? 'page' : undefined}>
                {section.label}
              </Link>
            )
          })}
        </nav>
        <span className="nav-count">精选</span>
      </header>
      <Outlet />
      <footer className="footer">Nex Club</footer>
    </div>
  )
}
