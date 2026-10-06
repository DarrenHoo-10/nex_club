import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { createRoutes } from '../routes.jsx'
import { createFixtureClient } from '../api/fixture.js'

function mount(extra = {}) {
  const api = { current: async () => ({ admin_id: 'admin' }), mcpTokens: async () => ({ items: [] }), mcpActions: async () => ({ items: [] }), ...extra }
  const router = createMemoryRouter(createRoutes({ adminApi: api, publicClient: createFixtureClient() }), { initialEntries: ['/admin/automation/mcp'] })
  render(<RouterProvider router={router} />)
  return api
}
it('gates a prepared publication behind preview and explicit confirmation', async () => {
  const action = { id: 'action', operation: 'publish', status: 'pending', expires_at: '2099-01-01', preview_digest: 'digest', arguments: { body: { edit_version: 7 } }, preview: { id: 'r', title: '待发布教程', summary: '确认前不可见', kind: 'tutorial', details: { level: 'beginner', steps: [] }, tags: [], card: {} } }
  const api = mount({ mcpActions: async () => ({ items: [action] }), mcpConfirm: vi.fn(async () => { action.status = 'completed'; action.result = { id: 'r' }; return { id: 'r' } }) })
  const user = userEvent.setup()
  await user.click(await screen.findByRole('button', { name: '操作预览与记录' }))
  const button = await screen.findByRole('button', { name: '确认发布内容' })
  expect(button).toBeDisabled()
  await user.click(screen.getByRole('button', { name: '预览效果' }))
  const dialog = await screen.findByRole('dialog', { name: '待发布教程' })
  expect(api.mcpConfirm).not.toHaveBeenCalled()
  await user.click(within(dialog).getByRole('button', { name: '返回编辑' }))
  await user.click(button)
  expect(api.mcpConfirm).toHaveBeenCalledWith('action', { reject: false, preview_digest: 'digest' })
  expect(await screen.findByRole('link', { name: '打开资源 ↗' })).toHaveAttribute('href', '/admin/resources/r')
})
it('uses the gateway without issuing Club tokens and keeps historical revocation', async () => {
  const token = { id: 'old-token', name: '旧客户端', revoked_at: null }
  const api = mount({ mcpTokens: async () => ({ items: [token] }), mcpNewToken: vi.fn(), mcpRevokeToken: vi.fn(async () => { token.revoked_at = '2026-10-07' }) })
  const user = userEvent.setup()
  expect(await screen.findByLabelText('网关服务地址')).toHaveValue('https://mcp.nexorai.com.cn/mcp')
  expect(screen.queryByRole('button', { name: '创建访问令牌' })).not.toBeInTheDocument()
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  await user.click(await screen.findByRole('button', { name: '撤销' }))
  expect(api.mcpRevokeToken).toHaveBeenCalledWith('old-token')
  expect(await screen.findByText('已撤销')).toBeInTheDocument()
  expect(api.mcpNewToken).not.toHaveBeenCalled()
})
