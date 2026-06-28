export type ApiClientConfig = {
  baseUrl: string
  getToken?: () => string | undefined
}

export type HealthResponse = {
  status: 'ok'
  version: string
}

export type LayoutPreferences = {
  sidebarPosition: 'left' | 'right'
  panels: { left: string[]; right: string[] }
  collapsed: { left: boolean; right: boolean }
}

export type UserPreferences = {
  theme: 'light' | 'dark' | 'system'
  layout: LayoutPreferences
}

export type User = {
  id: string
  username: string
  displayName?: string
}

export type ErrorResponse = {
  error: string
  code: string
  message: string
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: ErrorResponse,
  ) {
    super(body.message)
    this.name = 'ApiError'
  }
}

export function createApiClient(config: ApiClientConfig) {
  const { baseUrl, getToken } = config

  async function request<T>(
    path: string,
    init?: RequestInit,
  ): Promise<T> {
    const headers = new Headers(init?.headers)
    headers.set('Accept', 'application/json')
    if (init?.body) {
      headers.set('Content-Type', 'application/json')
    }
    const token = getToken?.()
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    }

    const response = await fetch(`${baseUrl}${path}`, {
      ...init,
      headers,
    })

    if (!response.ok) {
      const body = (await response.json().catch(() => ({
        error: 'unknown',
        code: 'unknown',
        message: response.statusText,
      }))) as ErrorResponse
      throw new ApiError(response.status, body)
    }

    if (response.status === 204) {
      return undefined as T
    }

    return (await response.json()) as T
  }

  return {
    getHealth: () => request<HealthResponse>('/api/v1/health'),
    getMe: () => request<User>('/api/v1/me'),
    getPreferences: () => request<UserPreferences>('/api/v1/preferences'),
    patchPreferences: (body: Partial<UserPreferences>) =>
      request<UserPreferences>('/api/v1/preferences', {
        method: 'PATCH',
        body: JSON.stringify(body),
      }),
  }
}

export type ApiClient = ReturnType<typeof createApiClient>
