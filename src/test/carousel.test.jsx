import { act, fireEvent, render, screen } from '@testing-library/react'
import Carousel from '../components/Carousel.jsx'

const slides = Array.from({ length: 5 }, (_, index) => ({
  id: `slide-${index}`,
  title: `推荐 ${index + 1}`,
  desc: `介绍 ${index + 1}`,
  tags: [],
  item: { id: `slide-${index}`, name: `推荐 ${index + 1}`, title: `推荐 ${index + 1}`, url: 'https://example.com', coverCount: 1 },
  onOpen: vi.fn(),
}))

function position(track) {
  return Number(track.style.getPropertyValue('--i'))
}

function endMove(track) {
  fireEvent(track, Object.assign(new Event('transitionend', { bubbles: true }), { propertyName: 'transform' }))
  act(() => vi.advanceTimersByTime(50))
}

describe('continuous recommendation carousel', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('crosses both ends by one adjacent slide, then rebases without animation', () => {
    const { container } = render(<Carousel slides={slides} kind="site" />)
    const track = container.querySelector('.car-track')
    const start = position(track)

    fireEvent.click(screen.getByRole('button', { name: '上一张', exact: true }))
    expect(position(track)).toBe(start - 1)
    expect(screen.getByRole('heading', { name: '推荐 5' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '第 5 张', exact: true })).toHaveAttribute('aria-current', 'true')
    fireEvent(track, Object.assign(new Event('transitionend', { bubbles: true }), { propertyName: 'transform' }))
    expect(track).toHaveClass('car-track-reset')
    act(() => vi.advanceTimersByTime(50))
    expect(track).not.toHaveClass('car-track-reset')

    const last = position(track)
    fireEvent.click(screen.getByRole('button', { name: '下一张', exact: true }))
    expect(position(track)).toBe(last + 1)
    expect(screen.getByRole('heading', { name: '推荐 1' })).toBeInTheDocument()
    endMove(track)
    expect(position(track)).toBe(start)
  })

  it('keeps the previous direction through repeated full loops and ignores overlapping moves', () => {
    const { container } = render(<Carousel slides={slides} kind="site" suspended />)
    const track = container.querySelector('.car-track')
    for (let count = 1; count <= 12; count += 1) {
      const before = position(track)
      fireEvent.click(screen.getByRole('button', { name: '上一张', exact: true }))
      fireEvent.click(screen.getByRole('button', { name: '上一张', exact: true }))
      expect(position(track)).toBe(before - 1)
      const expected = (5 - count % 5) % 5 + 1
      expect(screen.getByRole('heading', { name: `推荐 ${expected}` })).toBeInTheDocument()
      endMove(track)
    }
  })

  it('wraps autoplay forward and pauses while a detail is open', () => {
    const { container, rerender } = render(<Carousel slides={slides} kind="site" />)
    const track = container.querySelector('.car-track')
    for (let count = 1; count <= 5; count += 1) {
      const before = position(track)
      act(() => vi.advanceTimersByTime(5200))
      expect(position(track)).toBe(before + 1)
      endMove(track)
    }
    expect(screen.getByRole('heading', { name: '推荐 1' })).toBeInTheDocument()
    rerender(<Carousel slides={slides} kind="site" suspended />)
    const before = position(track)
    act(() => vi.advanceTimersByTime(10400))
    expect(position(track)).toBe(before)
  })

  it('uses the adjacent copy for the last dot and handles replacement and single slides', () => {
    const { container, rerender } = render(<Carousel slides={slides} kind="site" />)
    const track = container.querySelector('.car-track')
    const before = position(track)
    fireEvent.click(screen.getByRole('button', { name: '第 5 张', exact: true }))
    expect(position(track)).toBe(before - 1)
    endMove(track)
    rerender(<Carousel slides={slides.slice(1, 2)} kind="site" />)
    expect(screen.getByRole('heading', { name: '推荐 2' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '上一张', exact: true })).toBeDisabled()
    expect(screen.getByRole('button', { name: '下一张', exact: true })).toBeDisabled()
  })
})
