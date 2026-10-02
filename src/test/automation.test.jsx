import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { createRoutes } from '../routes.jsx'
import { createFixtureClient } from '../api/fixture.js'

function mount(path, api) {
  const client = { current: async () => ({ admin_id: 'admin', username: '管理员' }), ...api }
  const router = createMemoryRouter(createRoutes({ adminApi: client, publicClient: createFixtureClient() }), { initialEntries: [path] })
  render(<RouterProvider router={router} />)
  return router
}
const proposal = {
  id: 'suggestion', resource_id: 'resource', proposed_kind: 'tool', processing_run_id: 'stage', status: 'pending', base_edit_version: 2,
  proposed_payload: { title: '工具候选' },
  field_changes: { title: { old: '旧名称', new: '工具候选', locked: false, evidence: [{ excerpt: '原文标题' }] }, summary: { old: '人工简介', new: '模型简介', locked: true, evidence: [] } },
}
const preview = { id: 'preview', kind: 'tool', title: '预览标题', summary: '人工简介', details: {}, tags: [], card: { meta: '免费', href: 'https://example.com', subtitle: 'example.com' } }
function reviewApi(extra = {}) {
  return { getProposal: async () => proposal, listTags: async () => ({ tags: [] }), processingContext: async () => ({ source_name: '真实来源', body: '原始资料正文', stages: [] }), previewProposal: vi.fn(async () => preview), decideProposal: vi.fn(async () => ({ status: 'applied', resource_id: 'published' })), ...extra }
}

describe('automation workbench', () => {
  it('opens automation as the admin landing page and displays real counters', async () => {
    const router = mount('/admin', { automationOverview: async () => ({ pending_reviews: 7, enabled_sources: 2, sources: 3, new_items_24h: 9, blocked_stages: 1, active_fetches: 0, failed_fetches: 0, unknown_calls: 0, model: { mode: 'disabled' } }) })
    expect(await screen.findByText('7')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/admin/automation')
    expect(screen.getByText('7').closest('a')).toHaveAttribute('href', '/admin/automation/reviews')
  })
  it('creates a paused RSS source through the real-shaped API without implicitly starting a job', async () => {
    const user = userEvent.setup()
    const api = { sourceCapabilities: async () => ({ paid: [] }), listSources: async () => ({ items: [], has_more: false }), saveSource: vi.fn(async () => ({ id: 'source' })), runSource: vi.fn() }
    mount('/admin/automation/sources', api)
    await user.click(await screen.findByRole('button', { name: '添加信源', exact: true }))
    await user.type(screen.getByLabelText('信源名称'), '订阅测试')
    await user.selectOptions(screen.getByLabelText('来源类型'), 'rss')
    await user.type(screen.getByLabelText('订阅网址'), 'https://example.com/rss')
    await user.click(screen.getByRole('button', { name: '保存信源' }))
    expect(await screen.findByText('信源配置已保存')).toBeInTheDocument()
    expect(api.saveSource).toHaveBeenCalledWith(null, expect.objectContaining({ name: '订阅测试', kind: 'rss', feed_url: 'https://example.com/rss', enabled: false, interval_seconds: 3600, participation_mode: 'content' }))
    expect(api.runSource).not.toHaveBeenCalled()
  })
  it('requires preview of current decisions before publishing and preserves protected fields', async () => {
    const user = userEvent.setup(); const api = reviewApi()
    mount('/admin/automation/reviews/suggestion', api)
    const select = await screen.findByRole('combobox', { name: '标题处理方式' })
    expect(screen.getByRole('combobox', { name: '简介处理方式' })).toHaveValue('reject')
    const publish = screen.getByRole('button', { name: '采纳并发布', exact: true })
    expect(publish).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '预览采纳效果' }))
    const dialog = await screen.findByRole('dialog', { name: '预览标题' })
    expect(within(dialog).getByText('人工简介')).toBeInTheDocument()
    expect(api.previewProposal).toHaveBeenCalledWith('suggestion', expect.objectContaining({ fields: { title: 'accept', summary: 'reject' }, edit_version: 2 }))
    await user.click(within(dialog).getByRole('button', { name: '返回编辑' }))
    expect(publish).toBeEnabled()
    await user.selectOptions(select, 'rewrite')
    await user.clear(screen.getByLabelText('改写标题'))
    await user.type(screen.getByLabelText('改写标题'), '人工标题')
    expect(publish).toBeDisabled()
    await user.click(screen.getByRole('button', { name: '预览采纳效果' }))
    await screen.findByRole('dialog', { name: '预览标题' })
    await user.click(screen.getByRole('button', { name: '返回编辑' }))
    expect(api.decideProposal).not.toHaveBeenCalled()
    await user.click(publish)
    expect(api.decideProposal).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: '确认发布', exact: true }))
    expect(await screen.findByRole('link', { name: '查看已发布资源 →' })).toHaveAttribute('href', '/admin/resources/published')
    expect(api.decideProposal).toHaveBeenCalledWith('suggestion', expect.objectContaining({ fields: { title: 'rewrite', summary: 'reject' }, rewrites: { title: '人工标题' } }))
  })
  it('rejects the whole proposal explicitly without invoking preview or publication', async () => {
    const user = userEvent.setup(); const api = reviewApi({ decideProposal: vi.fn(async () => ({ status: 'rejected' })) })
    mount('/admin/automation/reviews/suggestion', api)
    await user.click(await screen.findByRole('button', { name: '拒绝整条建议' }))
    await user.click(screen.getByRole('button', { name: '确认拒绝', exact: true }))
    expect(await screen.findByText('已拒绝')).toBeInTheDocument()
    expect(api.decideProposal).toHaveBeenCalledWith('suggestion', expect.objectContaining({ fields: { title: 'reject', summary: 'reject' }, rewrites: {}, unlock: [] }))
    expect(api.previewProposal).not.toHaveBeenCalled()
  })
  it('prevents retrying an uncertain model call from the task page', async () => {
    mount('/admin/automation/tasks', { listProcessingRuns: async () => ({ items: [{ id: 'run', title: '工具资料', stage: 'write', status: 'blocked', rerun_no: 0, uncertain_call: true, error_message: '供应商结果不明' }] }) })
    expect(await screen.findByText('工具资料')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试本阶段' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '重新加工', exact: true })).toBeDisabled()
  })
})
