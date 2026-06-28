import { createApiClient } from '@repo/api-client'
import { getApiBaseUrl } from '#/env'

export const apiClient = createApiClient({
  baseUrl: getApiBaseUrl(),
})
