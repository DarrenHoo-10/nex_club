import { tolerateDetachedDom } from '../domTolerance.js'

it('skips removeChild and insertBefore when a browser has already moved the node', () => {
  tolerateDetachedDom()
  const first = document.createElement('div')
  const second = document.createElement('div')
  const node = document.createElement('span')
  first.appendChild(node)
  second.appendChild(node)
  expect(first.removeChild(node)).toBe(node)
  expect(second.insertBefore(node, document.createElement('i'))).toBe(node)
  expect(node.parentNode).toBe(second)
  expect(second.removeChild(node)).toBe(node)
  expect(node.parentNode).toBeNull()
})
