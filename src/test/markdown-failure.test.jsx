import { render, screen } from '@testing-library/react'
import { beforeEach, vi } from 'vitest'
import Reader from '../components/Reader.jsx'

vi.mock('react-markdown', () => ({
  default: function BrokenMarkdown() {
    throw new Error('parse failed')
  },
}))

beforeEach(() => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
})

it('shows the original text when markdown parsing fails', () => {
  render(<Reader item={{ title: '失败', levelLabel: '未知', minutes: 1, bodyMarkdown: '原文还在这里', steps: [], notes: null }} />)
  expect(screen.getByText('正文格式无法完整解析，以下为原文。')).toBeInTheDocument()
  expect(screen.getByText('原文还在这里')).toBeInTheDocument()
})
