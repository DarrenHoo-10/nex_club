export class ApiError extends Error {
  constructor({ status = 0, code, message, requestId = null, fieldErrors = [] }) {
    super(message || code || 'request failed')
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
    this.fieldErrors = fieldErrors
  }
}

export function buildQuery(params) {
  const query = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value == null || value === '') return
    if (Array.isArray(value)) {
      value.forEach((item) => {
        if (item != null && item !== '') query.append(key, String(item))
      })
      return
    }
    query.set(key, String(value))
  })
  const text = query.toString()
  return text ? `?${text}` : ''
}

export async function requestJson(fetchImpl, url, init, expectShape) {
  let res
  try {
    res = await fetchImpl(url, init)
  } catch (err) {
    if (err?.name === 'AbortError') throw err
    throw new ApiError({ status: 0, code: 'network', message: '网络不可用' })
  }
  const body = await parseResponse(res)
  if (expectShape && !expectShape(body)) {
    throw new ApiError({
      status: res.status,
      code: 'bad_response',
      message: '响应不符合契约',
      requestId: res.headers.get('x-request-id'),
    })
  }
  return body
}

async function parseResponse(res) {
  const requestId = res.headers.get('x-request-id')
  if (res.status === 204) return null
  const text = await res.text()
  let body = null
  if (text) {
    try {
      body = JSON.parse(text)
    } catch {
      throw new ApiError({ status: res.status, code: 'bad_response', message: '响应不是 JSON', requestId })
    }
  }
  if (!res.ok) {
    if (!body || typeof body !== 'object') {
      throw new ApiError({ status: res.status, code: 'bad_response', message: '响应不符合契约', requestId })
    }
    return throwApi(res.status, body, requestId)
  }
  return body
}

function throwApi(status, body, requestId) {
  throw new ApiError({
    status,
    code: typeof body.code === 'string' ? body.code : 'bad_response',
    message: typeof body.message === 'string' ? body.message : '',
    requestId: body.request_id || requestId,
    fieldErrors: Array.isArray(body.field_errors) ? body.field_errors : [],
  })
}
