export class ApiError extends Error {
  status: number
  code?: string
  constructor(status: number, message: string, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

let onUnauthorized: (() => void) | null = null
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(method: string, path: string, body?: unknown, opts: { quiet401?: boolean } = {}): Promise<T> {
  const headers: Record<string, string> = { 'X-Neo-Request': '1', Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  let res: Response
  try {
    res = await fetch(path, {
      method,
      headers,
      credentials: 'same-origin',
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'Cannot reach the server — check your connection.')
  }
  const text = await res.text()
  let data: any = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = text
    }
  }
  if (!res.ok) {
    if (res.status === 401 && !opts.quiet401 && onUnauthorized && !path.startsWith('/api/auth/')) onUnauthorized()
    const msg = (data && typeof data === 'object' && data.error) || (typeof data === 'string' && data) || res.statusText
    throw new ApiError(res.status, msg, data?.code)
  }
  return data as T
}

export const api = {
  get: <T>(p: string, opts?: { quiet401?: boolean }) => request<T>('GET', p, undefined, opts),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b ?? {}),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b ?? {}),
  del: <T>(p: string) => request<T>('DELETE', p),
}

/** Uploads an image (status page logo) and returns its URL on this server. */
export async function uploadImage(file: File): Promise<string> {
  const form = new FormData()
  form.append('file', file)
  let res: Response
  try {
    res = await fetch('/api/uploads', { method: 'POST', body: form, credentials: 'same-origin', headers: { 'X-Neo-Request': '1' } })
  } catch {
    throw new ApiError(0, 'Cannot reach the server — check your connection.')
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, data?.error || res.statusText)
  return data.url as string
}
