import { render, screen } from '@testing-library/react'
import Reader from '../components/Reader.jsx'

function renderReader(item) {
  return render(<Reader item={{ levelLabel: '入门', minutes: 3, notes: null, steps: [], bodyMarkdown: null, ...item }} />)
}

describe('Reader', () => {
  it('shows markdown when there are no steps', () => {
    renderReader({ title: '仅正文', bodyMarkdown: '这是完整正文段落。', steps: [] })
    expect(screen.getByText('这是完整正文段落。')).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })

  it('shows steps as plain text when there is no markdown', () => {
    renderReader({
      title: '仅步骤',
      bodyMarkdown: null,
      steps: ['<img src="x" onerror="window.__step = 1">'],
      notes: '**不是粗体**',
    })
    expect(screen.getByText('<img src="x" onerror="window.__step = 1">')).toBeInTheDocument()
    expect(screen.getByText('**不是粗体**')).toBeInTheDocument()
    expect(screen.queryByText('不是粗体')).not.toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()
    expect(window.__step).toBeUndefined()
  })

  it('shows body before steps when both exist', () => {
    renderReader({ title: '两者', bodyMarkdown: '正文段落', steps: ['步骤一'] })
    const body = screen.getByText('正文段落')
    const step = screen.getByText('步骤一')
    expect(body.compareDocumentPosition(step) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(step.closest('.reader-steps')).not.toBeNull()
    expect(step.closest('.reader-body')).toBeNull()
  })

  it('does not execute raw HTML or javascript links', () => {
    window.__xss = undefined
    renderReader({
      title: '安全',
      bodyMarkdown: [
        '可见正文',
        '',
        '<img src="x" onerror="window.__xss = 1">',
        '',
        '<div onclick="window.__xss = 1">点</div>',
        '',
        '[坏链接](javascript:alert(1))',
        '',
        '[官方文档](https://example.com/guide)',
      ].join('\n'),
    })
    expect(screen.getByText('可见正文')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '官方文档' })).toHaveAttribute('href', 'https://example.com/guide')
    expect(screen.queryByRole('link', { name: '坏链接' })).not.toBeInTheDocument()
    expect(document.querySelector('script')).toBeNull()
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelector('[onclick]')).toBeNull()
    expect(document.querySelector('[onerror]')).toBeNull()
    expect(window.__xss).toBeUndefined()
  })
})
