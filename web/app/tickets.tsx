import { useEffect, useMemo, useState } from 'react'
import qrcode from 'qrcode-generator'
import { api, currency, describe, type Hold, type Match } from './api'
import { Glyph, kickoff } from './booking-ui'

function QR({ value }: { value: string }) {
  const matrix = useMemo(() => {
    const qr = qrcode(0, 'M')
    qr.addData(value)
    qr.make()
    const size = qr.getModuleCount()
    const segments: string[] = []
    for (let row = 0; row < size; row++) {
      for (let col = 0; col < size; col++) {
        if (qr.isDark(row, col)) segments.push(`M${col + 4} ${row + 4}h1v1h-1z`)
      }
    }
    return { size: size + 8, path: segments.join('') }
  }, [value])
  return (
    <svg
      className="bx-qr"
      role="img"
      aria-label="Kode QR berisi ID tiket"
      viewBox={`0 0 ${matrix.size} ${matrix.size}`}
      shapeRendering="crispEdges"
    >
      <rect width={matrix.size} height={matrix.size} fill="white" />
      <path fill="#101f17" d={matrix.path} />
    </svg>
  )
}

function issuedTickets(hold: Hold) {
  return hold.tickets?.length
    ? hold.tickets
    : hold.ticketId
      ? [{ ticketId: hold.ticketId, ordinal: 1 }]
      : []
}

function downloadTicketDetails(match: Match, hold: Hold) {
  const section = match.sections.find((s) => s.id === hold.sectionId)
  const when = kickoff(match)
  const items = [
    'TRIBUNPASS · E-TIKET DEMO',
    match.name,
    when && when.date + ' · ' + when.time,
    'Tribun ' + (section?.name ?? '—'),
    'Pemesan: ' + (hold.displayName || 'Tamu'),
    'Nomor pesanan: ' + hold.holdId,
    'Total: ' + currency(hold.totalIdr ?? hold.priceIdr),
    '',
    ...issuedTickets(hold).map((t) => `Tiket ${t.ordinal}: ${t.ticketId}`),
  ]
  const url = URL.createObjectURL(
    new Blob([items.filter((v) => v != null).join('\n')], { type: 'text/plain;charset=utf-8' }),
  )
  const a = document.createElement('a')
  a.href = url
  a.download = 'TribunPass-' + hold.holdId.slice(0, 8) + '.txt'
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function TicketBundle({ match, hold }: { match: Match; hold: Hold }) {
  const section = match.sections.find((s) => s.id === hold.sectionId)
  const when = kickoff(match)
  const tickets = issuedTickets(hold)
  return (
    <div className="bx-bundle">
      <div className="bx-receipt-actions">
        <button className="bx-secondary" onClick={() => window.print()}>
          <Glyph name="print" />
          Cetak tiket
        </button>
        <button className="bx-text-button" onClick={() => downloadTicketDetails(match, hold)}>
          <Glyph name="down" />
          Unduh detail (.txt)
        </button>
      </div>
      {tickets.map((t) => (
        <article className="bx-eticket" key={t.ticketId}>
          <div className="bx-eticket-info">
            <div className="bx-passline">
              <Glyph name="ticket" /> E-tiket · {t.ordinal}/{tickets.length}
            </div>
            <h3>{match.name}</h3>
            <strong>Tribun {section?.name ?? '—'}</strong>
            {when && (
              <p>
                {when.date} · {when.time}
              </p>
            )}
            <p>Pemesan: {hold.displayName || 'Tamu'}</p>
            <code>{t.ticketId}</code>
          </div>
          <div className="bx-eticket-stub">
            <QR value={t.ticketId} />
            <span>Tiket demo</span>
          </div>
        </article>
      ))}
    </div>
  )
}

type OrderSummary = {
  holdId: string
  displayName: string
  sectionName: string
  quantity: number
  totalIdr: number
  status: Hold['status']
  createdAt: string
  expiresAt: string
}

export function Wallet({ match, resume }: { match: Match; resume: (h: Hold) => void }) {
  const [orders, setOrders] = useState<OrderSummary[]>([])
  const [details, setDetails] = useState<Hold | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState('')
  const [loading, setLoading] = useState(true)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let active = true
    async function load() {
      const { orders } = await api<{ orders: OrderSummary[] }>('me/orders')
      if (active) {
        setOrders(orders || [])
        setError('')
      }
    }
    setLoading(true)
    load()
      .catch((e) => {
        if (active) setError(describe(e))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [reload])

  async function open(order: OrderSummary) {
    if (details?.holdId === order.holdId) {
      setDetails(null)
      return
    }
    setBusy(order.holdId)
    setError('')
    try {
      const hold = await api<Hold>('holds/' + order.holdId)
      if (hold.status === 'HELD') {
        resume(hold)
        return
      }
      setOrders((existing) =>
        existing.map((o) => (o.holdId === hold.holdId ? { ...o, status: hold.status } : o)),
      )
      setDetails(hold.status === 'CONFIRMED' ? hold : null)
    } catch (e) {
      setError(describe(e))
    } finally {
      setBusy('')
    }
  }
  return (
    <section className="bx-wallet">
      <div className="bx-wallet-head">
        <h1>Tiket saya</h1>
        <p>Pesanan dari sesi demo di browser ini.</p>
      </div>
      {error && (
        <div className="bx-alert" role="alert">
          {error}{' '}
          <button className="bx-text-button" onClick={() => setReload((n) => n + 1)}>
            Muat ulang
          </button>
        </div>
      )}
      {loading ? (
        <p role="status">Memuat pesanan…</p>
      ) : orders.length === 0 && !error ? (
        <div className="bx-panel bx-empty">
          <Glyph name="ticket" size={36} />
          <h2>Belum ada pesanan</h2>
          <p>Pilih tribun di loket untuk mendapatkan tiket pertandingan.</p>
          <a href="/" className="bx-primary">
            Lihat pertandingan <Glyph name="arrow" />
          </a>
        </div>
      ) : (
        orders.map((h) => (
          <article
            className={'bx-panel bx-order' + (details?.holdId === h.holdId ? ' bx-open' : '')}
            key={h.holdId}
          >
            <div className="bx-order-top">
              <span className="bx-order-code">#{h.holdId.slice(0, 8).toUpperCase()}</span>
              <span className={'bx-status bx-status-' + h.status}>
                {h.status === 'HELD'
                  ? 'Menunggu pembayaran'
                  : h.status === 'CONFIRMED'
                    ? 'Terbit'
                    : 'Kedaluwarsa'}
              </span>
            </div>
            <h2>{match.name}</h2>
            <p>
              Tribun {h.sectionName} · {h.quantity} tiket · {currency(h.totalIdr)}
            </p>
            {h.status !== 'EXPIRED' && (
              <button
                className="bx-text-button"
                disabled={!!busy}
                aria-expanded={h.status === 'CONFIRMED' ? details?.holdId === h.holdId : undefined}
                onClick={() => open(h)}
              >
                {busy === h.holdId
                  ? 'Memuat…'
                  : h.status === 'HELD'
                    ? 'Lanjutkan pembayaran'
                    : details?.holdId === h.holdId
                      ? 'Tutup e-tiket'
                      : 'Lihat e-tiket'}
                <Glyph name={h.status === 'HELD' ? 'arrow' : 'ticket'} />
              </button>
            )}
            {details?.holdId === h.holdId && <TicketBundle match={match} hold={details} />}
          </article>
        ))
      )}
    </section>
  )
}
