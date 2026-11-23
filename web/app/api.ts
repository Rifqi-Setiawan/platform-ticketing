export type Section = {
  id: string
  name: string
  capacity: number
  available: number
  priceIdr: number
}
export type Match = { id: string; name: string; sections: Section[] }
export type HoldSnapshot = {
  holdId: string
  status: 'HELD' | 'CONFIRMED' | 'EXPIRED'
  priceIdr: number
  expiresAt: string
}
export type Transition = {
  sequence: number
  from?: string
  to: string
  cause: string
  at: string
}
export type Hold = HoldSnapshot & {
  sectionId: string
  ticketId?: string
  transitions: Transition[]
}
export type Payment = {
  eventId: string
  holdId: string
  outcome: string
  ticketId?: string
}
export type Scenario = 'last-ticket' | 'duplicate-payment' | 'late-payment'
export type ScenarioResult = {
  scenario: Scenario
  sectionId: string
  capacity: number
  available: number
  steps: {
    label: string
    outcome: string
    holdId?: string
    ticketId?: string
    eventId?: string
  }[]
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

export async function api<T>(
  path: string,
  body?: object,
  key?: string,
): Promise<T> {
  const response = await fetch('/v1/' + path, {
    method: body === undefined ? 'GET' : 'POST',
    cache: 'no-store',
    credentials: 'same-origin',
    signal: AbortSignal.timeout(15000),
    headers: {
      ...(body && { 'Content-Type': 'application/json' }),
      ...(key && { 'Idempotency-Key': key }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await response.json().catch(() => null)
  if (!response.ok) {
    throw new ApiError(
      response.status,
      data?.error?.code || 'UNAVAILABLE',
      data?.error?.message || 'The ticket service is unavailable.',
    )
  }
  if (data === null)
    throw new Error('The ticket service returned an unreadable response.')
  return data as T
}

export function describe(error: unknown) {
  if (error instanceof ApiError && error.code === 'SOLD_OUT')
    return 'That stand just sold out. Choose another stand or refresh availability.'
  if (error instanceof ApiError && error.status === 401)
    return 'Your demo session has expired. Reload to start a new one.'
  if (error instanceof ApiError && error.status === 429)
    return 'Too many requests. Wait a moment before trying again.'
  if (error instanceof ApiError && error.status < 500) return error.message
  return 'The ticket service could not be reached. Check your connection and retry.'
}

export const currency = (value: number) =>
  'Rp ' + new Intl.NumberFormat('id-ID').format(value)
export const human = (value: string) => value.replaceAll('_', ' ').toLowerCase()
export const clock = (value: string) =>
  new Date(value).toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })

const STORAGE = 'tribunpass.ticket.v1'
export type Saved = {
  sessionId: string
  holdId?: string
  paymentId?: string
  pending?: { key: string; sectionId: string }
}
export function readSaved(): Saved | null {
  try {
    return JSON.parse(localStorage.getItem(STORAGE) || 'null')
  } catch {
    return null
  }
}
export function save(value: Saved) {
  localStorage.setItem(STORAGE, JSON.stringify(value))
}

let opening: Promise<{ sessionId: string }> | undefined
export function openSession() {
  // Strict Mode may mount the screen twice before the first Set-Cookie response arrives.
  if (!opening)
    opening = api<{ sessionId: string }>('demo-sessions', {}).finally(() => {
      opening = undefined
    })
  return opening
}
