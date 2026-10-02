import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { createAdminFixture } from '../admin/fixture.js'
import { ApiError } from '../api/http.js'
import { createFixtureClient } from '../api/fixture.js'
import { createRoutes } from '../routes.jsx'

function renderAdmin(path, { publicClient = createFixtureClient(), adminApi }) {
  const router = createMemoryRouter(createRoutes({ publicClient, adminApi }), { initialEntries: [path] })
  render(<RouterProvider router={router} />)
  return router
}

function toolResource(extra = {}) {
  return {
    id: 'res-1',
    kind: 'tool',
    slug: 'claude',
    status: 'draft',
    edit_version: 2,
    field_locks: [],
    first_published_at: null,
    draft_revision_id: 'rev-1',
    published_revision_id: null,
    updated_at: '2026-10-01T00:00:00Z',
    draft: {
      revision_id: 'rev-1',
      title: 'Claude',
      aliases: [],
      summary: '原始简介',
      body_markdown: '',
      cover_urls: [],
      primary_category_id: null,
      tag_ids: [],
      quality_score: 0,
      recommendation_reason: '',
      details: { website_url: 'https://claude.ai', pricing: 'freemium', platforms: ['web'], deployment: ['hosted'] },
    },
    published: null,
    ...extra,
  }
}

function editorApi(resource, extra = {}) {
  return {
    current: async () => ({ admin_id: 'admin-1', username: 'admin', expires_at: '2026-10-02T00:00:00Z' }),
    listTags: async () => ({ tags: [] }),
    listRevisions: async () => ({ revisions: [] }),
    getResource: async () => resource,
    saveResource: async () => { throw new ApiError({ status: 409, code: 'edit_conflict', message: '内容已被他人更新' }) },
    preview: async () => extra.preview,
    ...extra,
  }
}

describe('admin', () => {
  it('preserves unsaved content warnings after a visibility change', async () => {
    const user = userEvent.setup()
    const resource = toolResource({ status: 'published', first_published_at: '2026-09-12T12:00:00Z', draft: null, published: toolResource().draft })
    const api = editorApi(resource, { setVisibility: async () => ({ edit_version: 3, status: 'hidden' }) })
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    try {
      const router = renderAdmin('/admin/resources/res-1', { adminApi: api })
      const summary = await screen.findByLabelText('简介')
      await user.type(summary, '尚未保存')
      await user.click(screen.getByRole('button', { name: '隐藏', exact: true }))
      await user.click(screen.getByRole('button', { name: '确认隐藏' }))
      await screen.findByRole('button', { name: '恢复公开' })
      await user.click(screen.getByRole('link', { name: '返回列表' }))
      await waitFor(() => expect(confirm).toHaveBeenCalled())
      expect(router.state.location.pathname).toBe('/admin/resources/res-1')
      expect(screen.getByLabelText('简介').value).toContain('尚未保存')
    } finally { confirm.mockRestore() }
  })
  it('sends an anonymous visitor from /admin/resources to login', async () => {
    const router = renderAdmin('/admin/resources', { adminApi: createAdminFixture() })
    expect(await screen.findByLabelText('用户名')).toBeInTheDocument()
    expect(router.state.location.pathname).toBe('/admin/login')
    expect(router.state.location.search).toContain('next=')
  })

  it('keeps the textarea after an edit conflict', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/resources/res-1', { adminApi: editorApi(toolResource()) })
    const summary = await screen.findByLabelText('简介')
    await user.type(summary, '未保存的句子')
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(await screen.findByText('内容已更新')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '加载服务器版本' })).toBeInTheDocument()
    expect(screen.getByLabelText('简介').value).toContain('未保存的句子')
  })

  it('gives tutorial preview steps to Reader', async () => {
    const user = userEvent.setup()
    const resource = toolResource({
      kind: 'tutorial',
      slug: 'steps-only',
      draft: {
        revision_id: 'rev-1',
        title: '带步骤',
        aliases: [],
        summary: '摘要',
        body_markdown: '已保存正文',
        cover_urls: [],
        primary_category_id: null,
        tag_ids: [],
        quality_score: 0,
        recommendation_reason: '',
        details: { level: 'beginner', minutes: 4, steps: ['表单里的步骤'], author: '', source_url: null, notes: '' },
      },
    })
    renderAdmin('/admin/resources/res-1', {
      adminApi: editorApi(resource, {
        preview: async () => ({
          id: 'res-1',
          kind: 'tutorial',
          slug: 'steps-only',
          title: '带步骤',
          summary: '摘要',
          cover_urls: [],
          cover_fallback_count: 3,
          tags: [],
          primary_category: null,
          quality_score: 0,
          first_published_at: null,
          content_updated_at: '2026-10-01T00:00:00Z',
          aliases: [],
          body_markdown: '预览正文',
          recommendation_reason: null,
          details: { level: 'beginner', minutes: 4, steps: ['独特步骤甲'], notes: '注意甲' },
          card: { subtitle: '4 分钟 · 1 步', meta: '入门', href: null, cta: '查看教程' },
        }),
      }),
    })
    await screen.findByLabelText('简介')
    await user.click(screen.getByRole('button', { name: '预览' }))
    const step = await screen.findByText('独特步骤甲')
    expect(step.closest('.reader-steps')).not.toBeNull()
    expect(step.closest('.reader-body')).toBeNull()
    expect(screen.getByText('已保存草稿 · 未发布')).toBeInTheDocument()
  })

  it('shows markdown-only tutorials in preview and on the public detail after publish', async () => {
    const user = userEvent.setup()
    const body = [
      '完整教程正文用于核对显示',
      '',
      '[官方文档](https://example.com/guide)',
      '',
      '[坏链接](javascript:alert(1))',
      '',
      '<img src="x" onerror="window.__xss = 1">',
    ].join('\n')
    const adminApi = createAdminFixture()
    await adminApi.login('admin', 'correct-password')
    const seeded = adminApi.seed({
      kind: 'tutorial',
      slug: 'only-markdown',
      status: 'draft',
      draft: {
        title: '仅正文教程',
        aliases: [],
        summary: '一篇只有正文的教程',
        body_markdown: body,
        cover_urls: [],
        primary_category_id: null,
        tag_ids: [],
        quality_score: 1,
        recommendation_reason: '',
        details: { level: 'beginner', minutes: 3, steps: [], author: '', source_url: null, notes: '' },
      },
    })
    const publicClient = {
      async listResources() {
        return { items: adminApi.publishedSnapshot(), next_cursor: null, has_more: false, total: null, effective_sort: 'recommended', ranking_version: null, ranking_computed_at: null, applied_query: { kind: 'tutorial', q: '', tags: [], sort: 'recommended' } }
      },
      async getBySlug(slug) {
        const found = adminApi.publishedSnapshot().find((item) => item.slug === slug)
        if (!found) throw new ApiError({ status: 404, code: 'not_found', message: 'missing' })
        return found
      },
      async listTags() { return { tags: [] } },
      async listFeatured() { return { items: [] } },
      async postEvents() { return { accepted: 1, duplicate: 0 } },
    }
    window.__xss = undefined
    const router = renderAdmin(`/admin/resources/${seeded.id}`, { adminApi, publicClient })
    await screen.findByLabelText('简介')
    await user.click(screen.getByRole('button', { name: '预览' }))
    expect(await screen.findByText('完整教程正文用于核对显示')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '官方文档' })).toHaveAttribute('href', 'https://example.com/guide')
    expect(screen.queryByRole('link', { name: '坏链接' })).not.toBeInTheDocument()
    expect(document.querySelector('[onerror]')).toBeNull()
    expect(window.__xss).toBeUndefined()

    await user.click(screen.getByRole('button', { name: '返回编辑' }))
    await user.click(screen.getByRole('button', { name: '发布' }))
    await user.click(screen.getByRole('button', { name: '确认发布' }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/resources/only-markdown')
    })
    expect(screen.getByRole('heading', { name: '仅正文教程' })).toBeInTheDocument()
    expect(screen.getByText('完整教程正文用于核对显示')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '官方文档' })).toHaveAttribute('href', 'https://example.com/guide')
    expect(screen.queryByRole('link', { name: '坏链接' })).not.toBeInTheDocument()
    expect(document.querySelector('script')).toBeNull()
    expect(window.__xss).toBeUndefined()
  })

  it('makes slug read-only after publication', async () => {
    const resource = toolResource({
      status: 'published',
      first_published_at: '2026-09-12T12:00:00Z',
      draft: null,
      draft_revision_id: null,
      published_revision_id: 'rev-1',
      published: toolResource().draft,
    })
    renderAdmin('/admin/resources/res-1', { adminApi: editorApi(resource) })
    const slug = await screen.findByLabelText('slug')
    expect(slug).toHaveProperty('readOnly', true)
    expect(screen.getByText('slug 在发布后保持不变')).toBeInTheDocument()
  })

  it('previews a new unsaved tool as a card and detail without writing or recording visitor events', async () => {
    const user = userEvent.setup()
    const api = editorApi(null, {
      createResource: vi.fn(), saveResource: vi.fn(), publish: vi.fn(), preview: vi.fn(),
    })
    const publicClient = createFixtureClient()
    publicClient.getBySlug = vi.fn(publicClient.getBySlug)
    publicClient.postEvents = vi.fn(publicClient.postEvents)
    const router = renderAdmin('/admin/resources/new?kind=tool', { adminApi: api, publicClient })
    await user.type(await screen.findByLabelText('标题'), '我的新工具')
    await user.type(screen.getByLabelText('简介'), '尚未保存的工具介绍')
    await user.type(screen.getByLabelText('官网'), 'https://example.com/new-tool')
    await user.click(screen.getByRole('button', { name: '预览', exact: true }))
    const dialog = await screen.findByRole('dialog', { name: '我的新工具' })
    expect(within(dialog).getByText('当前编辑内容 · 尚未保存')).toBeInTheDocument()
    expect(within(dialog).getByText('尚未保存的工具介绍')).toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: /example.com\/new-tool/ })).toHaveAttribute('href', 'https://example.com/new-tool')
    await user.click(within(dialog).getByRole('button', { name: '卡片效果' }))
    expect(within(dialog).getByRole('heading', { level: 3, name: '我的新工具' })).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '我的新工具', exact: true }))
    expect(within(dialog).getByRole('button', { name: '详情效果' })).toHaveAttribute('aria-pressed', 'true')
    await user.click(within(dialog).getByRole('button', { name: '返回编辑' }))
    expect(screen.getByLabelText('简介')).toHaveValue('尚未保存的工具介绍')
    expect(router.state.location.pathname).toBe('/admin/resources/new')
    expect(screen.getByRole('button', { name: '预览', exact: true })).toHaveFocus()
    for (const method of ['createResource', 'saveResource', 'publish', 'preview']) expect(api[method]).not.toHaveBeenCalled()
    expect(publicClient.getBySlug).not.toHaveBeenCalled()
    expect(publicClient.postEvents).not.toHaveBeenCalled()
  })

  it('uses current repository edits instead of an older saved preview and preserves dirty state', async () => {
    const user = userEvent.setup()
    const resource = toolResource({ kind: 'repo', draft: {
      ...toolResource().draft, title: '示例仓库',
      details: { full_name: 'sample/old', language: 'Go', license: 'MIT' },
    } })
    const api = editorApi(resource, { preview: vi.fn(), saveResource: vi.fn() })
    renderAdmin('/admin/resources/res-1', { adminApi: api })
    const name = await screen.findByLabelText('owner/name')
    await user.clear(name)
    await user.type(name, 'sample/new')
    await user.clear(screen.getByLabelText('简介'))
    await user.type(screen.getByLabelText('简介'), '新改的仓库介绍')
    await user.click(screen.getByRole('button', { name: '预览', exact: true }))
    const dialog = await screen.findByRole('dialog', { name: '示例仓库' })
    expect(within(dialog).getByText('新改的仓库介绍')).toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: /github.com\/sample\/new/ })).toHaveAttribute('href', 'https://github.com/sample/new')
    await user.click(within(dialog).getByRole('button', { name: '返回编辑' }))
    expect(name).toHaveValue('sample/new')
    expect(screen.getByRole('button', { name: '发布', exact: true })).toBeDisabled()
    expect(api.preview).not.toHaveBeenCalled()
    expect(api.saveResource).not.toHaveBeenCalled()
  })

  it('shows the latest unsaved tutorial body each time preview opens', async () => {
    const user = userEvent.setup()
    renderAdmin('/admin/resources/new?kind=tutorial', { adminApi: editorApi(null) })
    await user.type(await screen.findByLabelText('标题'), '教程预览')
    await user.type(screen.getByLabelText('Markdown 正文'), '## 第一次编写\n\n正文一')
    await user.click(screen.getByRole('button', { name: '预览', exact: true }))
    expect(await within(screen.getByRole('dialog')).findByRole('heading', { name: '第一次编写' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '返回编辑' }))
    await user.clear(screen.getByLabelText('Markdown 正文'))
    await user.type(screen.getByLabelText('Markdown 正文'), '## 第二次编写\n\n正文二')
    await user.click(screen.getByRole('button', { name: '预览', exact: true }))
    expect(await within(screen.getByRole('dialog')).findByRole('heading', { name: '第二次编写' })).toBeInTheDocument()
    expect(within(screen.getByRole('dialog')).queryByRole('heading', { name: '第一次编写' })).not.toBeInTheDocument()
  })

  it('ignores a late saved-preview response after leaving the editor', async () => {
    const user = userEvent.setup()
    let finishPreview
    const api = editorApi(toolResource(), {
      preview: () => new Promise((resolve) => { finishPreview = resolve }),
      listResources: async () => ({ items: [] }),
    })
    const router = renderAdmin('/admin/resources/res-1', { adminApi: api })
    await screen.findByLabelText('简介')
    await user.click(screen.getByRole('button', { name: '预览', exact: true }))
    await user.click(screen.getByRole('link', { name: '返回列表', exact: true }))
    expect(router.state.location.pathname).toBe('/admin/resources')
    await act(async () => finishPreview({ title: '迟到的预览' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
