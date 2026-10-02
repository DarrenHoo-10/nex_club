import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'

// jsdom has no top layer; real-browser checks cover focus containment and Escape.
if (!HTMLDialogElement.prototype.showModal) {
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
    this.querySelector('button')?.focus()
  }
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }
}

afterEach(() => {
  cleanup()
  document.cookie.split(';').forEach((part) => {
    const name = part.split('=')[0]?.trim()
    if (name) document.cookie = `${name}=; Max-Age=0; Path=/`
  })
})
