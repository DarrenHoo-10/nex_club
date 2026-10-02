import { lazy, Suspense } from 'react'
import { createBrowserRouter, Navigate, Outlet } from 'react-router-dom'
import { AdminApiSlot } from './api/adminSlot.js'
import { createDefaultPublicClient } from './api/client.js'
import { ApiProvider } from './api/context.jsx'
import IndexRedirect from './pages/IndexRedirect.jsx'
import PublicLayout from './pages/PublicLayout.jsx'
import ResourceDetail from './pages/ResourceDetail.jsx'
import SectionPage from './pages/SectionPage.jsx'

const AdminApp = lazy(() => import('./admin/AdminApp.jsx'))

export function createRoutes({ publicClient, adminApi } = {}) {
  const client = publicClient || createDefaultPublicClient()
  return [
    {
      element: (
        <ApiProvider client={client}>
          <AdminApiSlot.Provider value={adminApi || null}>
            <Outlet />
          </AdminApiSlot.Provider>
        </ApiProvider>
      ),
      children: [
        {
          element: <PublicLayout />,
          children: [
            { index: true, element: <IndexRedirect /> },
            { path: 'tools', element: <SectionPage sectionKey="tools" /> },
            { path: 'tutorials', element: <SectionPage sectionKey="tutorials" /> },
            { path: 'repos', element: <SectionPage sectionKey="repos" /> },
            { path: 'resources/:slug', element: <ResourceDetail /> },
          ],
        },
        { path: 'admin', element: <Navigate to="/admin/automation" replace /> },
        {
          path: 'admin/*',
          element: (
            <Suspense fallback={<p className="empty" aria-busy="true">加载中…</p>}>
              <AdminApp />
            </Suspense>
          ),
        },
      ],
    },
  ]
}

export function createAppRouter() {
  return createBrowserRouter(createRoutes())
}
