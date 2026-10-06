import { act, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { createFixtureClient } from '../api/fixture.js'
import { ApiError } from '../api/http.js'
import { createRoutes } from '../routes.jsx'

function renderAt(path, publicClient = createFixtureClient()) {
  const router = createMemoryRouter(createRoutes({ publicClient }), { initialEntries: [path] })
  render(<RouterProvider router={router} />)
  return router
}

function resource(title, extra = {}) {
  return {
    id: title,
    kind: 'tool',
    slug: title,
    title,
    summary: title,
    cover_urls: [],
    cover_fallback_count: 1,
    tags: [{ id: 'tag-chat', name: '对话', slug: 'chat', dimension: 'capability' }],
    card: { subtitle: 'example.com', meta: '免费', href: 'https://example.com', cta: '访问' },
    ...extra,
  }
}

function page(items, effectiveSort = 'recommended') {
  return {
    items,
    next_cursor: null,
    has_more: false,
    total: null,
    effective_sort: effectiveSort,
    ranking_version: null,
    ranking_computed_at: null,
    applied_query: { kind: 'tool', q: '', tags: [], sort: effectiveSort },
  }
}

function clientWith(listResources) {
  return {
    listResources,
    listTags: async () => ({ tags: [] }),
    listFeatured: async () => ({ items: [] }),
    postEvents: async () => ({ accepted: 0, duplicate: 0 }),
    getBySlug: async () => { throw new ApiError({ status: 404, code: 'not_found', message: 'missing' }) },
  }
}

describe('public sections', () => {
  it('opens tutorials by default with tutorials first in the navigation', async () => {
    const router = renderAt('/')
    await screen.findByRole('heading', { level: 1, name: '照着做就能成的 AI 教程' })
    expect(router.state.location.pathname).toBe('/tutorials')
    const links = within(screen.getByRole('navigation', { name: '板块' })).getAllByRole('link')
    expect(links.map(link => link.textContent)).toEqual(['AI教程', 'AI工具网站', 'AI GitHub 项目'])
    expect(links.map(link => link.getAttribute('href'))).toEqual(['/tutorials', '/tools', '/repos'])
    expect(links[0]).toHaveAttribute('aria-current', 'page')
  })

  it('returns to tutorials from the logo without changing section URLs', async () => {
    const user = userEvent.setup()
    const router = renderAt('/repos?sort=latest')
    await screen.findByRole('heading', { level: 1, name: '值得 Star 的 AI 开源项目' })
    await user.click(screen.getByRole('link', { name: 'Nex Club' }))
    await screen.findByRole('heading', { level: 1, name: '照着做就能成的 AI 教程' })
    expect(router.state.location.pathname).toBe('/tutorials')
    await act(() => router.navigate(-1))
    expect(router.state.location.pathname + router.state.location.search).toBe('/repos?sort=latest')
  })

  it('renders the tools fixture', async () => {
    renderAt('/tools')
    expect(await screen.findByRole('heading', { level: 1, name: '发现真正好用的 AI 工具' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 3, name: 'Claude' }).length).toBeGreaterThan(0)
  })

  it('renders the tutorials fixture', async () => {
    renderAt('/tutorials')
    expect(await screen.findByRole('heading', { level: 1, name: '照着做就能成的 AI 教程' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 3, name: '如何订阅 Claude 会员' }).length).toBeGreaterThan(0)
  })

  it('renders the repos fixture', async () => {
    renderAt('/repos')
    expect(await screen.findByRole('heading', { level: 1, name: '值得 Star 的 AI 开源项目' })).toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 3, name: 'ollama' }).length).toBeGreaterThan(0)
  })

  it('drops a slow list response after the query changes', async () => {
    const user = userEvent.setup()
    const calls = []
    let resolveSlow = () => {}
    const router = renderAt('/tools', clientWith((params) => {
      const q = params.q || ''
      calls.push(q)
      if (q === 'a') return new Promise((resolve) => { resolveSlow = () => resolve(page([resource('慢结果')], 'relevance')) })
      if (q === 'ab') return Promise.resolve(page([resource('快结果')], 'relevance'))
      return Promise.resolve(page([resource('初始')]))
    }))
    expect(router.state.location.pathname).toBe('/tools')
    await screen.findByRole('heading', { name: '初始' })
    await user.type(screen.getByRole('searchbox', { name: '搜索' }), 'ab')
    expect(await screen.findByRole('heading', { name: '快结果' })).toBeInTheDocument()
    resolveSlow()
    await screen.findByRole('heading', { name: '快结果' })
    expect(screen.queryByRole('heading', { name: '慢结果' })).not.toBeInTheDocument()
    expect(calls).toContain('ab')
  })

  it('presses 最新 when effective_sort is latest', async () => {
    renderAt('/tools', clientWith(async () => page([resource('Claude')], 'latest')))
    expect(await screen.findByRole('button', { name: '最新' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '推荐' })).toHaveAttribute('aria-pressed', 'false')
  })

  it.each([
    ['tools', '发现真正好用的 AI 工具'],
    ['tutorials', '照着做就能成的 AI 教程'],
    ['repos', '值得 Star 的 AI 开源项目'],
  ])('preserves the legacy #%s entry', async (key, title) => {
    const router = renderAt(`/#${key}`)
    expect(await screen.findByRole('heading', { level: 1, name: title })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe(`/${key}`)
    expect(router.state.location.hash).toBe('')
  })

  it('shows the unavailable copy for a missing detail', async () => {
    renderAt('/resources/missing')
    expect(await screen.findByRole('heading', { name: '这份内容已经下架或不存在' })).toBeInTheDocument()
    const sectionLinks = screen.getAllByRole('link', { name: 'AI工具网站' })
    expect(sectionLinks.length).toBeGreaterThan(0)
    sectionLinks.forEach((link) => expect(link).toHaveAttribute('href', '/tools'))
  })

  it('writes the tag slug into the query', async () => {
    const user = userEvent.setup()
    const router = renderAt('/tools')
    expect((await screen.findAllByRole('heading', { name: 'Claude' })).length).toBeGreaterThan(0)
    await user.click(screen.getAllByRole('button', { name: '对话' })[0])
    expect(router.state.location.search).toContain('tag=chat')
  })

  it('keeps sort and clears q when switching sections', async () => {
    const user = userEvent.setup()
    const router = renderAt('/tools?q=claude&tag=chat&sort=heat')
    await screen.findByRole('heading', { level: 1, name: '发现真正好用的 AI 工具' })
    await user.click(screen.getByRole('link', { name: 'AI教程' }))
    expect(router.state.location.pathname).toBe('/tutorials')
    expect(router.state.location.search).toBe('?sort=heat')
  })
})

describe('resource detail modal', () => {
  it.each([
    ['/tools?q=Claude&sort=latest', 'Claude', 'https://claude.ai/'],
    ['/tutorials?q=Claude&sort=latest', '如何订阅 Claude 会员', 'https://claude.ai/'],
    ['/repos?q=ollama&sort=latest', 'ollama', 'https://github.com/ollama/ollama'],
  ])('opens %s in place and restores focus after closing', async (path, title, href) => {
    const user = userEvent.setup()
    const client = createFixtureClient()
    client.postEvents = vi.fn(client.postEvents)
    const router = renderAt(path, client)
    const trigger = await screen.findByRole('button', { name: title, exact: true })
    expect(within(trigger.closest('article')).queryByRole('link')).not.toBeInTheDocument()
    await user.click(trigger)
    const dialog = await screen.findByRole('dialog', { name: title })
    const link = await within(dialog).findByRole('link', { name: new RegExp(href.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) })
    expect(link).toHaveAttribute('href', href)
    expect(link).toHaveAttribute('target', '_blank')
    expect(client.postEvents.mock.calls.flat(2).map((event) => event.type)).toEqual(['detail_view'])
    expect(router.state.location.pathname + router.state.location.search).toBe(path)
    expect(document.body.style.overflow).toBe('hidden')
    if (path.startsWith('/tutorials')) {
      expect(within(dialog).getByText('打开 claude.ai 注册。')).toBeInTheDocument()
      expect(within(dialog).getByText('通过官方页面完成订阅。')).toBeInTheDocument()
    }
    await user.click(within(dialog).getByRole('button', { name: '收起' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
    expect(document.body.style.overflow).toBe('')
  })

  it('keeps cover controls and tags independent while opening from the card body', async () => {
    const user = userEvent.setup()
    const router = renderAt('/tools?q=Claude')
    const trigger = await screen.findByRole('button', { name: 'Claude', exact: true })
    const card = trigger.closest('article')
    await user.click(within(card).getByRole('button', { name: '下一张封面' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await user.click(within(card).getByRole('button', { name: '对话' }))
    expect(router.state.location.search).toContain('tag=chat')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    await user.click(within(card).getByText('Anthropic 出品的 AI 助手'))
    expect(await screen.findByRole('dialog', { name: 'Claude' })).toBeInTheDocument()
  })

  it('opens a featured card from the keyboard and handles native Escape cancellation', async () => {
    const user = userEvent.setup()
    renderAt('/tools')
    const carousel = await screen.findByRole('region', { name: '推荐' })
    const trigger = await within(carousel).findByRole('button', { name: 'Claude', exact: true })
    act(() => trigger.focus())
    await user.keyboard('{Enter}')
    const dialog = await screen.findByRole('dialog', { name: 'Claude' })
    fireEvent(dialog, new Event('cancel', { cancelable: true }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trigger).toHaveFocus()
  })

  it('retries detail loading without leaving the list', async () => {
    const user = userEvent.setup()
    const client = createFixtureClient()
    client.getBySlug = vi.fn(client.getBySlug).mockRejectedValueOnce(new Error('offline'))
    renderAt('/repos?q=ollama', client)
    await user.click(await screen.findByRole('button', { name: 'ollama', exact: true }))
    const dialog = await screen.findByRole('dialog', { name: 'ollama' })
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('介绍加载失败')
    await user.click(within(dialog).getByRole('button', { name: '重试' }))
    expect(await within(dialog).findByRole('link', { name: /github.com\/ollama\/ollama/ })).toBeInTheDocument()
    expect(client.getBySlug).toHaveBeenCalledTimes(2)
  })

  it('does not show stale links when a listed resource is no longer available', async () => {
    const user = userEvent.setup()
    renderAt('/tools', clientWith(async () => page([resource('已下架')])))
    await user.click(await screen.findByRole('button', { name: '已下架', exact: true }))
    const dialog = await screen.findByRole('dialog', { name: '已下架' })
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('这份内容已经下架或不存在')
    expect(within(dialog).queryByRole('link')).not.toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '收起' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('cancels an unfinished detail request when closed', async () => {
    const user = userEvent.setup()
    const client = createFixtureClient()
    let resolveDetail
    let requestSignal
    client.getBySlug = (_slug, { signal }) => {
      requestSignal = signal
      return new Promise((resolve) => { resolveDetail = resolve })
    }
    client.postEvents = vi.fn(client.postEvents)
    renderAt('/tools?q=Claude', client)
    await user.click(await screen.findByRole('button', { name: 'Claude', exact: true }))
    await user.click(screen.getByRole('button', { name: '收起' }))
    expect(requestSignal.aborted).toBe(true)
    await act(async () => resolveDetail(resource('迟到的详情')))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(client.postEvents).not.toHaveBeenCalled()
  })
})
