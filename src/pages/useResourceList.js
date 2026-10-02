import { useCallback, useEffect, useRef, useState } from 'react'
import { toCardModel } from '../api/view.js'

export function useResourceList(client, { kind, q, tag, sort }) {
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [effectiveSort, setEffectiveSort] = useState(null)
  const [total, setTotal] = useState(null)
  const [hasMore, setHasMore] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)
  const seq = useRef(0)
  const cursorRef = useRef(null)

  useEffect(() => {
    const ctrl = new AbortController()
    const my = ++seq.current
    setLoading(true)
    setError(null)
    client.listResources({ kind, q, tag, sort }, { signal: ctrl.signal })
      .then((page) => {
        if (my !== seq.current) return
        setItems((page.items || []).map(toCardModel))
        setEffectiveSort(page.effective_sort)
        setTotal(typeof page.total === 'number' ? page.total : null)
        setHasMore(Boolean(page.has_more))
        cursorRef.current = page.next_cursor || null
        setLoading(false)
      })
      .catch((err) => {
        if (my !== seq.current || err?.name === 'AbortError') return
        setError(err)
        setLoading(false)
      })
    return () => ctrl.abort()
  }, [client, kind, q, tag, sort, reloadKey])

  const loadMore = useCallback(async () => {
    const cursor = cursorRef.current
    if (!cursor) return
    const my = seq.current
    setLoading(true)
    try {
      const page = await client.listResources({ kind, q, tag, sort, cursor })
      if (my !== seq.current) return
      setItems((prev) => {
        const seen = new Set(prev.map((item) => item.id))
        const extra = (page.items || []).map(toCardModel).filter((item) => !seen.has(item.id))
        return prev.concat(extra)
      })
      setHasMore(Boolean(page.has_more))
      cursorRef.current = page.next_cursor || null
      if (page.effective_sort) setEffectiveSort(page.effective_sort)
      setLoading(false)
    } catch (err) {
      if (my !== seq.current || err?.name === 'AbortError') return
      if (err?.code === 'cursor_stale') {
        setReloadKey((key) => key + 1)
        return
      }
      setError(err)
      setLoading(false)
    }
  }, [client, kind, q, tag, sort])

  const reload = useCallback(() => setReloadKey((key) => key + 1), [])

  return { items, loading, error, effectiveSort, total, hasMore, loadMore, reload }
}
