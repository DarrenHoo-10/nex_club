import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { createFixtureClient } from '../api/fixture.js'
import { createRoutes } from '../routes.jsx'
import MarkdownBody from '../components/MarkdownBody.jsx'
import tutorials from '../data/tutorials.json'
import { readFileSync } from 'node:fs'

const readerStyles = readFileSync('src/styles.css', 'utf8')

beforeEach(() => sessionStorage.clear())
afterEach(() => vi.restoreAllMocks())

it('preserves paragraph breaks, headings, emphasis, lists, quotes, tables and literal code', () => {
  const { container } = render(<MarkdownBody source={'### 章节\n\n第一行\n第二行\n\n独立段落 **重点** 与 *强调*。\n\n- 项目一\n- 项目二\n\n> 提示内容\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\n```js\nconst x = 1\n  console.log(x)\n```'} />)
  expect(screen.getByRole('heading', { level: 3, name: '章节' })).toBeInTheDocument()
  expect(container.querySelector('p').textContent).toBe('第一行\n第二行')
  expect(container.querySelector('strong')).toHaveTextContent('重点')
  expect(container.querySelector('em')).toHaveTextContent('强调')
  expect(container.querySelectorAll('ul > li')).toHaveLength(2)
  expect(container.querySelector('blockquote')).toHaveTextContent('提示内容')
  expect(screen.getByRole('table')).toBeInTheDocument()
  expect(screen.getByRole('region', { name: '表格（可横向滚动）' })).toHaveAttribute('tabindex', '0')
  expect(container.querySelector('pre code').textContent).toBe('const x = 1\n  console.log(x)\n')
})

it('assigns stable unique Chinese heading IDs without treating fenced code as headings', () => {
  const { container, rerender } = render(<MarkdownBody source={'## 安装 **教程**\n\n## 安装 教程\n\n```md\n## 假标题\n```'} />)
  expect([...container.querySelectorAll('h2')].map((node) => node.id)).toEqual(['section-安装-教程', 'section-安装-教程-2'])
  rerender(<MarkdownBody source={'## 安装 **教程**\n\n## 安装 教程'} />)
  expect([...container.querySelectorAll('h2')].map((node) => node.id)).toEqual(['section-安装-教程', 'section-安装-教程-2'])
})

it('keeps source emphasis semantic and visibly bold, including links and inline code', () => {
  const { container } = render(<><style>{readerStyles}</style><MarkdownBody source={'普通正文。**作者强调**。\n\n**[加粗链接](https://example.com)** 与 **`加粗代码`**。\n\n&#28857;**「按钮名称」**。'} /></>)
  const strong = [...container.querySelectorAll('.reader-body strong')]
  expect(strong.map(node => node.textContent)).toEqual(['作者强调', '加粗链接', '加粗代码', '「按钮名称」'])
  strong.forEach(node => expect(getComputedStyle(node).fontWeight).toBe('700'))
  expect(screen.getByRole('link', { name: '加粗链接' }).closest('strong')).not.toBeNull()
  expect(container.querySelector('strong code')).toHaveTextContent('加粗代码')
  expect(container.querySelector('p').textContent).toBe('普通正文。作者强调。')
})

it('preserves the ChatCut author’s verified emphasis in both local content sources', async () => {
  // Verified against the author's rendered X article, not inferred from prose:
  // https://x.com/xaiwind/status/2106734008436707477
  const article = await createFixtureClient().getBySlug('chatcut-video-agent')
  expect(tutorials.find(item => item.id === article.slug).body).toBe(article.body_markdown)
  const { container } = render(<MarkdownBody source={article.body_markdown} />)
  const strong = [...container.querySelectorAll('strong')]
  expect(strong).toHaveLength(89)
  expect(strong.map(node => node.textContent)).toEqual(expect.arrayContaining([
    'ChatCut 是一个用说话来剪的 AI 剪辑工具。',
    '剪映给的是工具，ChatCut 给的是结果。',
    '「打开剪映专业版」', '「重启剪映专业版」',
    '注意它交出去的是草稿工程，不是成片。',
  ]))
  // These are plain in the original, although older summaries emphasized them.
  expect(strong.some(node => node.textContent === 'ChatCut')).toBe(false)
  expect(strong.some(node => node.textContent.includes('它和剪映不是一回事'))).toBe(false)
  expect(container.querySelector('p').textContent).toContain('一个人做出海，工具就是同事。')
})

it('keeps local images and fragment links, rejecting unsafe URL schemes', () => {
  render(<MarkdownBody source={'![图](/covers/guide.jpg)\n\n[章节](#section-安装)\n\n[危险](javascript:alert(1))\n\n![坏图](data:image/svg+xml,test)'} />)
  expect(screen.getByRole('img', { name: '图' })).toHaveAttribute('src', '/covers/guide.jpg')
  expect(decodeURIComponent(screen.getByRole('link', { name: '章节' }).getAttribute('href'))).toBe('#section-安装')
  expect(screen.queryByRole('link', { name: '危险' })).not.toBeInTheDocument()
  expect(screen.queryByRole('img', { name: '坏图' })).not.toBeInTheDocument()
})

it('plays a markdown video file and keeps ordinary images as images', () => {
  const poster = 'https://cdn.example.com/poster.jpg'
  const file = 'https://cdn.example.com/clip.mp4?tag=29'
  const { container } = render(<MarkdownBody source={`![工作台动效](${file} "${poster}")\n\n![静帧](https://cdn.example.com/frame.jpg)\n\n![坏视频](javascript:alert(1).mp4)\n\n![本地视频](/covers/demo.webm)`} />)
  const videos = [...container.querySelectorAll('video')]
  expect(videos).toHaveLength(2)
  expect(videos[0]).toHaveAttribute('src', file)
  expect(videos[0]).toHaveAttribute('poster', poster)
  expect(videos[0]).toHaveAttribute('controls')
  expect(videos[0]).toHaveAttribute('aria-label', '工作台动效')
  expect(videos[0].parentElement.tagName).toBe('DIV')
  expect(videos[0].closest('p')).toBeNull()
  expect(videos[1]).toHaveAttribute('src', '/covers/demo.webm')
  expect(videos[1].hasAttribute('poster')).toBe(false)
  expect(screen.getByRole('img', { name: '静帧' })).toHaveAttribute('src', 'https://cdn.example.com/frame.jpg')
  expect(screen.queryByRole('img', { name: '坏视频' })).not.toBeInTheDocument()
  expect(container.querySelector('video[src^="javascript:"]')).toBeNull()
})

function renderTutorial(client) {
  const router = createMemoryRouter(createRoutes({ publicClient: client }), { initialEntries: ['/tutorials?q=ChatCut&sort=latest'] })
  render(<RouterProvider router={router} />)
  return router
}

it('uses real article headings for both outlines without changing the modal route or duplicating the cover', async () => {
  const user = userEvent.setup()
  const client = createFixtureClient()
  const article = await client.getBySlug('chatcut-video-agent')
  const router = renderTutorial(client)
  await user.click(await screen.findByRole('button', { name: article.title, exact: true }))
  const dialog = await screen.findByRole('dialog', { name: article.title })
  await within(dialog).findByRole('heading', { name: '十、优势' })
  expect(dialog.querySelectorAll('.reader-side-outline a')).toHaveLength(17)
  expect(dialog.querySelectorAll('.reader-mobile-outline a')).toHaveLength(17)
  expect(dialog.querySelectorAll('.resource-modal-cover, .reader-cover')).toHaveLength(1)
  expect(dialog.querySelectorAll('.reader-body img')).toHaveLength(5)
  expect(dialog.querySelectorAll('.reader-body pre')).toHaveLength(14)
  expect(dialog.querySelectorAll('.reader-steps li')).toHaveLength(5)
  expect(within(dialog).getByText(article.details.notes)).toBeInTheDocument()
  expect(router.state.location.pathname + router.state.location.search).toBe('/tutorials?q=ChatCut&sort=latest')
  await user.click(within(dialog.querySelector('.reader-side-outline')).getByRole('link', { name: '十、优势' }))
  expect(within(dialog).getByRole('heading', { name: '十、优势' })).toHaveFocus()
  expect(router.state.location.hash).toBe('')
})

it('tracks container scrolling and restores the chapter offset on reopening', async () => {
  const user = userEvent.setup()
  const client = createFixtureClient()
  const original = client.getBySlug
  client.getBySlug = async (...args) => ({ ...await original(...args), body_markdown: '### 第一章\n\n正文一\n\n### 第二章\n\n正文二' })
  const originalRect = Element.prototype.getBoundingClientRect
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function () {
    const root = this.closest('.resource-modal-content')
    if (this.matches('.resource-modal-content')) return { top: 100, height: this.closest('dialog')?.open ? 600 : 0 }
    if (root && this.id.startsWith('section-')) return { top: 100 + (this.id === 'section-第一章' ? 200 : 1000) - root.scrollTop, height: 30 }
    return originalRect.call(this)
  })
  renderTutorial(client)
  const title = '超简单 ChatCut 使用，Codex大白话剪辑出爆款视频'
  const trigger = await screen.findByRole('button', { name: title, exact: true })
  await user.click(trigger)
  const dialog = await screen.findByRole('dialog', { name: title })
  await within(dialog).findByRole('heading', { name: '第二章' })
  const root = dialog.querySelector('.resource-modal-content')
  await user.click(within(dialog.querySelector('.reader-side-outline')).getByRole('link', { name: '第二章' }))
  expect(root.scrollTop).toBe(976)
  root.scrollTop = 1120
  fireEvent.scroll(root)
  await waitFor(() => expect(JSON.parse(sessionStorage.getItem('nex-reading:tutorial:chatcut-video-agent')).top).toBe(1120))
  expect(dialog.querySelector('.reader-side-outline [aria-current]')).toHaveTextContent('第二章')
  await user.click(within(dialog).getByRole('button', { name: '收起' }))
  expect(trigger).toHaveFocus()
  await user.click(trigger)
  const reopened = await screen.findByRole('dialog', { name: title })
  await within(reopened).findByRole('heading', { name: '第二章' })
  expect(reopened.querySelector('.resource-modal-content').scrollTop).toBe(1120)
})
