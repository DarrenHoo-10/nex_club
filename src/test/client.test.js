import { createClient } from '../api/client.js'

function page() {
  return {
    items: [],
    next_cursor: null,
    has_more: false,
    total: null,
    effective_sort: 'recommended',
    ranking_version: null,
    ranking_computed_at: null,
    applied_query: { kind: 'tool', q: '', tags: [], sort: 'recommended' },
  }
}

it('sends list filters on the query string', async () => {
  let url = ''
  const client = createClient({
    fetchImpl: async (input) => {
      url = String(input)
      return new Response(JSON.stringify(page()), { status: 200 })
    },
  })
  await client.listResources({ kind: 'tool', q: 'claude', tag: 'chat', sort: 'latest' })
  expect(url).toBe('/api/v1/resources?kind=tool&q=claude&tag=chat&sort=latest')
})

it('maps network failures and invalid JSON', async () => {
  const offline = createClient({ fetchImpl: async () => { throw new TypeError('offline') } })
  await expect(offline.listResources({ kind: 'tool' })).rejects.toMatchObject({ code: 'network', status: 0 })

  const bad = createClient({
    fetchImpl: async () => new Response('nope', { status: 200 }),
  })
  await expect(bad.listResources({ kind: 'tool' })).rejects.toMatchObject({ code: 'bad_response' })
})

it('posts events with the caller supplied id', async () => {
  let body = ''
  const client = createClient({
    fetchImpl: async (_url, init) => {
      body = init.body
      return new Response(JSON.stringify({ accepted: 1, duplicate: 0 }), { status: 202 })
    },
  })
  await client.postEvents([{ id: 'evt-1', resource_id: 'res-1', type: 'detail_view' }])
  expect(JSON.parse(body)).toEqual({ events: [{ id: 'evt-1', resource_id: 'res-1', type: 'detail_view' }] })
})
