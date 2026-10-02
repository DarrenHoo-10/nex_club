import { buildQuery, requestJson } from './http.js'
import { createFixtureClient } from './fixture.js'

export function createClient({ baseUrl = '', fetchImpl = globalThis.fetch } = {}) {
  const root = baseUrl.replace(/\/$/, '')

  function get(path, { signal, expectShape } = {}) {
    return requestJson(fetchImpl, `${root}${path}`, {
      method: 'GET',
      headers: { Accept: 'application/json' },
      signal,
    }, expectShape)
  }

  return {
    listResources(params = {}, { signal } = {}) {
      const query = buildQuery({
        kind: params.kind,
        q: params.q,
        tag: params.tag,
        sort: params.sort,
        cursor: params.cursor,
        limit: params.limit,
      })
      return get(`/api/v1/resources${query}`, {
        signal,
        expectShape: (body) => body && Array.isArray(body.items) && typeof body.effective_sort === 'string' && typeof body.has_more === 'boolean',
      })
    },
    getBySlug(slug, { signal } = {}) {
      return get(`/api/v1/resources/by-slug/${encodeURIComponent(slug)}`, {
        signal,
        expectShape: (body) => body && typeof body.slug === 'string' && typeof body.title === 'string' && body.card,
      })
    },
    listTags(params = {}, { signal } = {}) {
      return get(`/api/v1/tags${buildQuery({ kind: params.kind, q: params.q })}`, {
        signal,
        expectShape: (body) => body && Array.isArray(body.tags),
      })
    },
    listFeatured(kind, { signal } = {}) {
      return get(`/api/v1/featured${buildQuery({ kind })}`, {
        signal,
        expectShape: (body) => body && Array.isArray(body.items),
      })
    },
    postEvents(events) {
      return requestJson(fetchImpl, `${root}/api/v1/events`, {
        method: 'POST',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
        body: JSON.stringify({ events }),
      })
    },
  }
}

export function createDefaultPublicClient() {
  if (import.meta.env.VITE_API_MODE === 'fixture') return createFixtureClient()
  return createClient({ baseUrl: '' })
}
