import { createAdminApi } from '../admin/api.js'

function json(status, body) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } })
}

it('sends CSRF and reuses one idempotency key when a write times out', async () => {
  const keys = []
  const uuid = vi.fn(() => 'same-key')
  let attempts = 0
  document.cookie = 'nex_csrf=csrf-token'
  const api = createAdminApi({
    uuid,
    fetchImpl: async (_url, init) => {
      attempts += 1
      keys.push(init.headers['Idempotency-Key'])
      expect(init.headers['X-CSRF-Token']).toBe('csrf-token')
      expect(init.credentials).toBe('include')
      if (attempts === 1) throw new TypeError('timed out')
      return json(200, { id: 'res-1', edit_version: 3, revision_id: 'rev-2' })
    },
  })
  await api.saveResource('res-1', { edit_version: 2, title: 'Claude' })
  expect(uuid).toHaveBeenCalledTimes(1)
  expect(keys).toEqual(['same-key', 'same-key'])
})

it('retries 503 once and does not retry 400 or 409', async () => {
  document.cookie = 'nex_csrf=csrf-token'
  let calls = 0
  const busy = createAdminApi({
    uuid: () => 'retry-key',
    fetchImpl: async () => {
      calls += 1
      if (calls === 1) return json(503, { code: 'request_busy', message: 'busy', request_id: 'r', field_errors: [] })
      return json(200, { id: 'res-1', edit_version: 2, revision_id: 'rev-2' })
    },
  })
  await busy.saveResource('res-1', { edit_version: 1, title: 'A' })
  expect(calls).toBe(2)

  let rejected = 0
  const invalid = createAdminApi({
    fetchImpl: async () => {
      rejected += 1
      return json(400, { code: 'invalid_argument', message: 'bad', request_id: 'r', field_errors: [{ field: 'slug', code: 'taken' }] })
    },
  })
  await expect(invalid.saveResource('res-1', { title: 'A' })).rejects.toMatchObject({
    status: 400,
    fieldErrors: [{ field: 'slug', code: 'taken' }],
  })
  expect(rejected).toBe(1)

  let conflicts = 0
  const conflicted = createAdminApi({
    fetchImpl: async () => {
      conflicts += 1
      return json(409, { code: 'edit_conflict', message: '内容已被他人更新', request_id: 'r', field_errors: [] })
    },
  })
  await expect(conflicted.saveResource('res-1', { edit_version: 1 })).rejects.toMatchObject({ status: 409, code: 'edit_conflict' })
  expect(conflicts).toBe(1)
})

it('does not attach an idempotency key to login', async () => {
  let initSeen
  const api = createAdminApi({
    fetchImpl: async (_url, init) => {
      initSeen = init
      return json(200, { admin_id: 'a', username: 'admin', expires_at: '2026-10-02T00:00:00Z' })
    },
  })
  await api.login('admin', 'secret')
  expect(initSeen.headers['Idempotency-Key']).toBeUndefined()
  expect(initSeen.credentials).toBe('include')
  expect(initSeen.method).toBe('POST')
})
