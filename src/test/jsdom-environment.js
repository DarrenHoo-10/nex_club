import { builtinEnvironments } from 'vitest/environments'

// jsdom 27+ ships its own AbortSignal. React Router passes that signal to
// Node's Request, which rejects it. Capture the Node constructors before
// the jsdom environment copies window globals onto the test global.
const NativeAbortController = globalThis.AbortController
const NativeAbortSignal = globalThis.AbortSignal

function useNativeAbort(target) {
  target.AbortController = NativeAbortController
  target.AbortSignal = NativeAbortSignal
}

export default {
  name: 'jsdom',
  transformMode: 'web',
  async setupVM(options) {
    const result = await builtinEnvironments.jsdom.setupVM(options)
    useNativeAbort(result.getVmContext())
    return result
  },
  async setup(global, options) {
    const result = await builtinEnvironments.jsdom.setup(global, options)
    useNativeAbort(global)
    return result
  },
}
