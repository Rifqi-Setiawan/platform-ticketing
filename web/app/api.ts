export type Section = {
  id: string
  name: string
  capacity: number
  available: number
  priceIdr: number
}
export type Match = { id: string; name: string; startsAt?: string; sections: Section[] }
export type HoldSnapshot = {
  holdId: string
  status: 'HELD' | 'CONFIRMED' | 'EXPIRED'
  priceIdr: number
  quantity?: number
  totalIdr?: number
  displayName?: string
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
  tickets?: { ticketId: string; ordinal: number }[]
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

export async function api<T>(path: string, body?: object, key?: string): Promise<T> {
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
  if (data === null) throw new Error('The ticket service returned an unreadable response.')
  return data as T
}

export function describe(error: unknown) {
  if (error instanceof ApiError && error.code === 'SOLD_OUT')
    return 'Kuota yang tersisa tidak mencukupi. Silakan kurangi jumlah atau pilih tribun lain.'
  if (error instanceof ApiError && error.status === 401)
    return 'Sesi demo berakhir. Muat ulang halaman untuk memulai sesi baru.'
  if (error instanceof ApiError && error.status === 429)
    return 'Permintaan terlalu sering. Tunggu sebentar, lalu coba lagi.'
  if (error instanceof ApiError && error.status < 500) return error.message
  return 'Koneksi ke loket terputus. Periksa jaringan lalu coba lagi.'
}

export const currency = (value: number) => 'Rp ' + new Intl.NumberFormat('id-ID').format(value)
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
  pending?: { key: string; sectionId: string; quantity?: number; displayName?: string }
}
export function readSaved(): Saved | null {
  try {
    return JSON.parse(localStorage.getItem(STORAGE) || 'null')
  } catch {
    return null
  }
}
export function save(value: Saved) {
  try {
    localStorage.setItem(STORAGE, JSON.stringify(value))
  } catch {
    // Active checkout still works in memory when the browser blocks storage.
  }
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
