import { createAdminFixture } from './fixture.js'
import { requestJson } from '../api/http.js'

export function readCsrfCookie(cookie = globalThis.document?.cookie || '') {
  const parts = cookie.split(';')
  for (const part of parts) {
    const trimmed = part.trim()
    const eq = trimmed.indexOf('=')
    if (eq === -1) continue
    if (trimmed.slice(0, eq) !== 'nex_csrf') continue
    return decodeURIComponent(trimmed.slice(eq + 1))
  }
  return ''
}

export function createAdminApi({ baseUrl = '', fetchImpl = globalThis.fetch, uuid = () => crypto.randomUUID() } = {}) {
  const root = baseUrl.replace(/\/$/, '')

  function send(path, { method, body, idempotencyKey, signal }) {
    const headers = { Accept: 'application/json' }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (method !== 'GET' && method !== 'HEAD') headers['X-CSRF-Token'] = readCsrfCookie()
    if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey
    return requestJson(fetchImpl, `${root}${path}`, {
      method,
      headers,
      body,
      credentials: 'include',
      signal,
    })
  }

  // One automatic retry for network failures and 503. The same key and body are reused.
  async function write(path, method, payload) {
    const key = uuid()
    const body = payload === undefined ? undefined : JSON.stringify(payload)
    const attempt = () => send(path, { method, body, idempotencyKey: key })
    try {
      return await attempt()
    } catch (err) {
      if (err?.name === 'AbortError') throw err
      if (err?.code === 'network' || err?.status === 503) return attempt()
      throw err
    }
  }

  const queryGet = (path, params = {}) => {
    const query = new URLSearchParams(Object.entries(params).filter(([, value]) => value !== '' && value != null))
    return send(`${path}${query.size ? `?${query}` : ''}`, { method: 'GET' })
  }
  return {
    mcpAction: (id) => queryGet(`/api/admin/mcp/actions/${encodeURIComponent(id)}`),
    mcpActions: () => queryGet('/api/admin/mcp/actions'),
    mcpConfirm: (id, body) => write(`/api/admin/mcp/actions/${encodeURIComponent(id)}/confirm`, 'POST', body),
    mcpTokens: () => queryGet('/api/admin/mcp/tokens'),
    mcpNewToken: (body) => send('/api/admin/mcp/tokens', { method: 'POST', body: JSON.stringify(body) }),
    mcpRevokeToken: (id) => write(`/api/admin/mcp/tokens/${encodeURIComponent(id)}`, 'DELETE', {}),
    automationOverview: () => queryGet('/api/admin/automation'),
    sourceCapabilities: () => queryGet('/api/admin/source-capabilities'),
    sourcePresets: () => queryGet('/api/admin/source-presets'),
    importSourcePresets: (ids) => write('/api/admin/source-presets/import', 'POST', { ids }),
    previewSource: (body) => write('/api/admin/sources/preview', 'POST', body),
    listSources: (params) => queryGet('/api/admin/sources', params),
    saveSource: (id, body) => write(`/api/admin/sources${id ? `/${encodeURIComponent(id)}` : ''}`, id ? 'PUT' : 'POST', body),
    runSource: (id, body) => write(`/api/admin/sources/${encodeURIComponent(id)}/run`, 'POST', body),
    listIngestRuns: (params) => queryGet('/api/admin/automation/ingest-runs', params),
    listProcessingRuns: (params) => queryGet('/api/admin/automation/processing-runs', params),
    processingContext: (id) => queryGet(`/api/admin/automation/processing-runs/${encodeURIComponent(id)}`),
    retryProcessing: (id) => write(`/api/admin/automation/processing-runs/${encodeURIComponent(id)}/retry`, 'POST', {}),
    rerunProcessing: (id) => write(`/api/admin/automation/processing-runs/${encodeURIComponent(id)}/rerun`, 'POST', {}),
    automationProposals: (params) => queryGet('/api/admin/automation/proposals', params),
    automationCalls: (params) => queryGet('/api/admin/automation/calls', params),
    getProposal: (id) => queryGet(`/api/admin/proposals/${encodeURIComponent(id)}`),
    previewProposal: (id, body) => send(`/api/admin/proposals/${encodeURIComponent(id)}/preview`, { method: 'POST', body: JSON.stringify(body) }),
    decideProposal: (id, body) => write(`/api/admin/proposals/${encodeURIComponent(id)}/decision`, 'POST', body),
    login(username, password) {
      return send('/api/admin/session', {
        method: 'POST',
        body: JSON.stringify({ username, password }),
      })
    },
    logout() {
      return write('/api/admin/session', 'DELETE')
    },
    current() {
      return send('/api/admin/session', { method: 'GET' })
    },
    listResources(params = {}) {
      const query = new URLSearchParams()
      if (params.kind) query.set('kind', params.kind)
      if (params.status) query.set('status', params.status)
      if (params.q) query.set('q', params.q)
      const text = query.toString()
      return send(`/api/admin/resources${text ? `?${text}` : ''}`, { method: 'GET' })
    },
    getResource(id) {
      return send(`/api/admin/resources/${encodeURIComponent(id)}`, { method: 'GET' })
    },
    createResource(body) {
      return write('/api/admin/resources', 'POST', body)
    },
    saveResource(id, body) {
      return write(`/api/admin/resources/${encodeURIComponent(id)}`, 'PUT', body)
    },
    publish(id, body) {
      return write(`/api/admin/resources/${encodeURIComponent(id)}/publish`, 'POST', body)
    },
    setVisibility(id, body) {
      return write(`/api/admin/resources/${encodeURIComponent(id)}/visibility`, 'POST', body)
    },
    preview(id) {
      return send(`/api/admin/resources/${encodeURIComponent(id)}/preview`, { method: 'GET' })
    },
    listRevisions(id) {
      return send(`/api/admin/resources/${encodeURIComponent(id)}/revisions`, { method: 'GET' })
    },
    listTags() {
      return send('/api/admin/tags', { method: 'GET' })
    },
    createTag(body) {
      return write('/api/admin/tags', 'POST', body)
    },
    mergeTag(id, body) {
      return write(`/api/admin/tags/${encodeURIComponent(id)}/merge`, 'POST', body)
    },
    listFeatured() {
      return send('/api/admin/featured', { method: 'GET' })
    },
    createFeatured(body) {
      return write('/api/admin/featured', 'POST', body)
    },
    deleteFeatured(id) {
      return write(`/api/admin/featured/${encodeURIComponent(id)}`, 'DELETE')
    },
  }
}

export function createDefaultAdminApi() {
  if (import.meta.env.VITE_API_MODE === 'fixture') return createAdminFixture()
  return createAdminApi()
}
