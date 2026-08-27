import type { components } from './generated/schema'

const QUEUE_EVENT_RECONNECT_DELAY_MS = 1000
const QUEUE_EVENTS_PATH = '/api/v1/playback/queue/events'

export type QueueEvent = components['schemas']['QueueEvent']

export type QueueEventSource = {
  addEventListener: (
    name: string,
    listener: (event: MessageEvent<string>) => void,
  ) => void
  close: () => void
  onerror: ((event: Event) => void) | null
}

export type QueueEventSubscription = {
  getBaseUrl: () => string | Promise<string>
  getToken?: () => string | undefined
  transport: typeof fetch
  eventSourceFactory: (url: string) => QueueEventSource
  subscriber?: (
    onEvent: (event: QueueEvent) => void,
    onError: (error: Error) => void,
  ) => (() => void) | Promise<() => void>
}

export function subscribeQueueEvents(
  config: QueueEventSubscription,
  onEvent: (event: QueueEvent) => void,
  onError?: (error: Error) => void,
): () => void {
  const reportError = onError ?? ((error: Error) => {
    console.warn('Queue event stream error', { error })
  })
  if (config.subscriber) {
    return subscribeWithCustomSubscriber(config.subscriber, onEvent, reportError)
  }
  if (config.getToken) {
    return subscribeWithFetch(config, onEvent, reportError)
  }
  return subscribeWithEventSource(config, onEvent, reportError)
}

function subscribeWithCustomSubscriber(
  subscriber: NonNullable<QueueEventSubscription['subscriber']>,
  onEvent: (event: QueueEvent) => void,
  reportError: (error: Error) => void,
): () => void {
  let unsubscribe: (() => void) | undefined
  let isClosed = false
  void Promise.resolve(subscriber(onEvent, reportError))
    .then((cleanup) => {
      if (isClosed) cleanup()
      else unsubscribe = cleanup
    })
    .catch((error) => reportError(toError(error)))
  return () => {
    isClosed = true
    unsubscribe?.()
  }
}

function subscribeWithEventSource(
  config: QueueEventSubscription,
  onEvent: (event: QueueEvent) => void,
  reportError: (error: Error) => void,
): () => void {
  let eventSource: QueueEventSource | undefined
  let isClosed = false
  void Promise.resolve(config.getBaseUrl())
    .then((baseUrl) => {
      if (isClosed) return
      eventSource = config.eventSourceFactory(buildQueueEventsUrl(baseUrl))
      eventSource.addEventListener('queue-invalidated', (event) => {
        try {
          onEvent(parseQueueEvent(event.data))
        } catch (error) {
          reportError(toError(error))
        }
      })
      eventSource.onerror = () => {
        reportError(new Error('Queue event stream disconnected'))
      }
    })
    .catch((error) => reportError(toError(error)))
  return () => {
    isClosed = true
    eventSource?.close()
  }
}

function subscribeWithFetch(
  config: QueueEventSubscription,
  onEvent: (event: QueueEvent) => void,
  reportError: (error: Error) => void,
): () => void {
  const abortController = new AbortController()
  void runFetchLoop(config, onEvent, reportError, abortController.signal)
  return () => abortController.abort()
}

async function runFetchLoop(
  config: QueueEventSubscription,
  onEvent: (event: QueueEvent) => void,
  reportError: (error: Error) => void,
  signal: AbortSignal,
): Promise<void> {
  let lastEventId: string | undefined
  while (!signal.aborted) {
    try {
      const headers = new Headers({ Accept: 'text/event-stream' })
      const token = config.getToken?.()
      if (token) headers.set('Authorization', `Bearer ${token}`)
      if (lastEventId) headers.set('Last-Event-ID', lastEventId)
      const response = await config.transport(
        buildQueueEventsUrl(await config.getBaseUrl()),
        { headers, signal },
      )
      lastEventId = await readQueueEventStream(response, onEvent, lastEventId)
    } catch (error) {
      if (signal.aborted) return
      reportError(toError(error))
    }
    await waitForReconnect(signal)
  }
}

async function readQueueEventStream(
  response: Response,
  onEvent: (event: QueueEvent) => void,
  lastEventId?: string,
): Promise<string | undefined> {
  if (!response.ok || !response.body) {
    throw new Error(`Queue event stream returned HTTP ${response.status}`)
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) return lastEventId
    buffer += decoder.decode(value, { stream: true }).replaceAll('\r\n', '\n')
    let frameEnd = buffer.indexOf('\n\n')
    while (frameEnd >= 0) {
      const frame = buffer.slice(0, frameEnd)
      buffer = buffer.slice(frameEnd + 2)
      const decoded = decodeQueueEventFrame(frame)
      if (decoded) {
        lastEventId = decoded.id
        onEvent(decoded.event)
      }
      frameEnd = buffer.indexOf('\n\n')
    }
  }
}

function decodeQueueEventFrame(
  frame: string,
): { id: string; event: QueueEvent } | undefined {
  let id = ''
  let name = ''
  const data: string[] = []
  for (const line of frame.split('\n')) {
    if (line.startsWith('id: ')) id = line.slice(4)
    else if (line.startsWith('event: ')) name = line.slice(7)
    else if (line.startsWith('data: ')) data.push(line.slice(6))
  }
  if (!id || name !== 'queue-invalidated') return undefined
  return { id, event: parseQueueEvent(data.join('\n')) }
}

function parseQueueEvent(data: string): QueueEvent {
  const event: unknown = JSON.parse(data)
  if (
    typeof event !== 'object' ||
    event === null ||
    !('revision' in event) ||
    typeof event.revision !== 'string' ||
    !('sequence' in event) ||
    typeof event.sequence !== 'string' ||
    !/^\d+$/.test(event.sequence) ||
    !('invalidates' in event) ||
    !Array.isArray(event.invalidates) ||
    !event.invalidates.every((value) => value === 'queue')
  ) {
    throw new TypeError('Queue event has an invalid payload')
  }
  return event as QueueEvent
}

function buildQueueEventsUrl(baseUrl: string): string {
  return `${baseUrl.replace(/\/$/, '')}${QUEUE_EVENTS_PATH}`
}

function waitForReconnect(signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve()
      return
    }
    const handleAbort = () => {
      clearTimeout(timeoutId)
      resolve()
    }
    const timeoutId = setTimeout(() => {
      signal.removeEventListener('abort', handleAbort)
      resolve()
    }, QUEUE_EVENT_RECONNECT_DELAY_MS)
    signal.addEventListener('abort', handleAbort, { once: true })
  })
}

function toError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error))
}
