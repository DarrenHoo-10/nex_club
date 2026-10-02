import { useMemo } from 'react'
import { Link, NavLink, Outlet, Route, Routes, useNavigate } from 'react-router-dom'
import MCPPage from './integrations/MCPPage.jsx'
import Logo from '../components/Logo.jsx'
import { useAdminApiSlot } from '../api/adminSlot.js'
import { createDefaultAdminApi } from './api.js'
import FeaturedPage from './pages/FeaturedPage.jsx'
import LoginPage from './pages/LoginPage.jsx'
import ResourceEditorPage from './pages/ResourceEditorPage.jsx'
import ResourceListPage from './pages/ResourceListPage.jsx'
import TagsPage from './pages/TagsPage.jsx'
import { AutomationLayout } from './automation/common.jsx'
import OverviewPage from './automation/OverviewPage.jsx'
import SourcesPage from './automation/SourcesPage.jsx'
import TasksPage from './automation/TasksPage.jsx'
import ReviewsPage from './automation/ReviewsPage.jsx'
import ReviewPage from './automation/ReviewPage.jsx'
import UsagePage from './automation/UsagePage.jsx'
import { AdminSessionProvider, RequireAuth, useAdminSession } from './session.jsx'

export default function AdminApp() {
  const slot = useAdminApiSlot()
  const api = useMemo(() => slot || createDefaultAdminApi(), [slot])
  return (
    <AdminSessionProvider api={api}>
      <div className="page">
        <div className="bg" aria-hidden><i className="b1" /><i className="b2" /><i className="b3" /><i className="b4" /></div>
        <Routes>
          <Route element={<AdminShell />}>
            <Route path="login" element={<LoginPage />} />
            <Route element={<RequireAuth />}>
              <Route index element={<OverviewPage />} />
              <Route path="automation" element={<AutomationLayout />}>
                <Route index element={<OverviewPage />} />
                <Route path="sources" element={<SourcesPage />} />
                <Route path="tasks" element={<TasksPage />} />
                <Route path="reviews" element={<ReviewsPage />} />
                <Route path="reviews/:id" element={<ReviewPage />} />
                <Route path="usage" element={<UsagePage />} />
                <Route path="mcp" element={<MCPPage />} />
              </Route>

              <Route path="resources" element={<ResourceListPage />} />
              <Route path="resources/new" element={<ResourceEditorPage />} />
              <Route path="resources/:id" element={<ResourceEditorPage />} />
              <Route path="tags" element={<TagsPage />} />
              <Route path="featured" element={<FeaturedPage />} />
            </Route>
          </Route>
        </Routes>
      </div>
    </AdminSessionProvider>
  )
}

function AdminShell() {
  const { admin, logout } = useAdminSession()
  const navigate = useNavigate()
  return (
    <div className="admin">
      <header className="nav glass">
        <Link className="logo" to="/tools">
          <Logo />
          <span className="logo-text">Nex <b>Club</b></span>
        </Link>
        <nav className="tabs" aria-label="管理">
          <NavLink className={({ isActive }) => `tab${isActive ? ' on' : ''}`} to="/admin/automation">自动化</NavLink>
          <NavLink className={({ isActive }) => `tab${isActive ? ' on' : ''}`} to="/admin/resources">资源</NavLink>
          <NavLink className={({ isActive }) => `tab${isActive ? ' on' : ''}`} to="/admin/tags">标签</NavLink>
          <NavLink className={({ isActive }) => `tab${isActive ? ' on' : ''}`} to="/admin/featured">推荐位</NavLink>
        </nav>
        {admin ? (
          <button
            type="button"
            className="pill"
            onClick={async () => {
              try {
                await logout()
              } catch {
                // Session cleanup still returns to the login form.
              }
              navigate('/admin/login', { replace: true })
            }}
          >
            退出
          </button>
        ) : <span className="nav-count">管理</span>}
      </header>
      <Outlet />
    </div>
  )
}
