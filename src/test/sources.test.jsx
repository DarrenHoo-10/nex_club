import { render, screen, within, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { createRoutes } from '../routes.jsx'
import { createFixtureClient } from '../api/fixture.js'

function mount(extra = {}) {
  const api = { current: async () => ({ admin_id: 'admin', username: '管理员' }), listSources: async () => ({ items: [], has_more: false }), sourceCapabilities: async () => ({ paid: [] }), ...extra }
  const router = createMemoryRouter(createRoutes({ adminApi: api, publicClient: createFixtureClient() }), { initialEntries: ['/admin/automation/sources'] })
  render(<RouterProvider router={router} />)
  return api
}
const previewResult = { total: 1, elapsed_ms: 20, warnings: [], paid: false, persisted_items: 0, processing_started: false, items: [{ title: '原文标题', url: 'https://example.com/article', summary: '<script>bad()</script>', published_at: null }] }

describe('source presets and trial fetches', () => {
  it('previews an unsaved feed without saving, enqueueing or running a model', async () => {
    const user = userEvent.setup()
    const api = mount({ previewSource: vi.fn(async () => previewResult), saveSource: vi.fn(), runSource: vi.fn() })
    await user.click(await screen.findByRole('button', { name: '添加信源', exact: true }))
    await user.selectOptions(screen.getByLabelText('来源类型'), 'rss')
    await user.type(screen.getByLabelText('订阅网址'), 'https://example.com/feed')
    await user.click(screen.getByRole('button', { name: '试抓预览', exact: true }))
    const dialog = await screen.findByRole('dialog', { name: '试抓结果' })
    expect(within(dialog).getByRole('link', { name: /原文标题/ })).toHaveAttribute('href', 'https://example.com/article')
    expect(within(dialog).getByText('原文日期未知')).toBeInTheDocument()
    expect(within(dialog).getByText(/没有写入资料库/)).toBeInTheDocument()
    expect(document.querySelector('script')).toBeNull()
    expect(api.saveSource).not.toHaveBeenCalled()
    expect(api.runSource).not.toHaveBeenCalled()
    await user.click(within(dialog).getByRole('button', { name: '返回配置' }))
    expect(screen.getByLabelText('订阅网址')).toHaveValue('https://example.com/feed')
  })
  it('keeps a date-only source date without timezone conversion', async () => {
    const user = userEvent.setup()
    mount({ previewSource: async () => ({ ...previewResult, items: [{ ...previewResult.items[0], published_date: '2026-09-24', published_at: '2026-09-24T00:00:00Z' }] }) })
    await user.click(await screen.findByRole('button', { name: '添加信源', exact: true }))
    await user.selectOptions(screen.getByLabelText('来源类型'), 'rss')
    await user.type(screen.getByLabelText('订阅网址'), 'https://example.com/feed')
    await user.click(screen.getByRole('button', { name: '试抓预览' }))
    expect(await screen.findByText('原文日期：2026-09-24')).toBeInTheDocument()
  })
  it('imports only selected unimported presets', async () => {
    const user = userEvent.setup()
    let imported = false
    const entries = [{ id: 'one', name: '已有源', kind: 'rss', imported: true, feed_url: 'https://example.com/one', interval_seconds: 3600 }, { id: 'two', name: '新示范源', kind: 'rss', feed_url: 'https://example.com/two', interval_seconds: 7200 }]
    const api = mount({ sourcePresets: async () => ({ items: entries.map((entry) => ({ ...entry, imported: entry.imported || imported })), upstream_url: 'https://github.com/KKKKhazix/AIHOT' }), importSourcePresets: vi.fn(async () => { imported = true; return { created: 1, existing: 0 } }) })
    await user.click(await screen.findByRole('button', { name: 'AIHOT 示范信源' }))
    expect(await screen.findByRole('checkbox', { name: '已有源' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '全选未导入' }))
    await user.click(screen.getByRole('button', { name: '导入所选 1 个' }))
    expect(api.importSourcePresets).toHaveBeenCalledWith(['two'])
    expect(await screen.findByText('已导入 1 个暂停信源，跳过 0 个已有信源')).toBeInTheDocument()
  })
  it('exposes the paid channel but disables paid requests when credentials are missing', async () => {
    const user = userEvent.setup()
    mount({ sourceCapabilities: async () => ({ paid: [{ kind: 'x', ready: false, credential_configured: false, required_env: ['SOCIALDATA_API_KEY'] }] }) })
    await user.click(await screen.findByRole('button', { name: '添加信源', exact: true }))
    await user.selectOptions(screen.getByLabelText('来源类型'), 'x')
    expect(await screen.findByText('等待服务商配置')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '试抓预览' })).toBeDisabled()
    expect(screen.getByRole('checkbox', { name: '启用采集' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '保存信源' })).toBeEnabled()
  })
  it('ignores a late preview after the configuration changes', async () => {
    const user = userEvent.setup()
    let resolvePreview
    mount({ previewSource: () => new Promise((resolve) => { resolvePreview = resolve }) })
    await user.click(await screen.findByRole('button', { name: '添加信源', exact: true }))
    await user.selectOptions(screen.getByLabelText('来源类型'), 'rss')
    await user.type(screen.getByLabelText('订阅网址'), 'https://example.com/old')
    await user.click(screen.getByRole('button', { name: '试抓预览' }))
    await user.clear(screen.getByLabelText('订阅网址'))
    await user.type(screen.getByLabelText('订阅网址'), 'https://example.com/new')
    await act(async () => resolvePreview(previewResult))
    expect(screen.queryByRole('dialog', { name: '试抓结果' })).not.toBeInTheDocument()
  })
})
