import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import { currency, type Hold, type Match, type Section } from './api'

const strokes = {
  arrow: 'M4 12h15m-6-6 6 6-6 6',
  back: 'M20 12H5m6-6-6 6 6 6',
  check: 'm5 12 4 4L19 6',
  calendar: 'M4 8h16M8 3v4m8-4v4M4 5h16v16H4z',
  clock: 'M12 7v6l4 2',
  ticket: 'M3 6h18v4a2 2 0 0 0 0 4v4H3v-4a2 2 0 0 0 0-4zM15 7v2m0 2v2m0 2v2',
  wallet: 'M3 6h17v14H3zM3 6l14-3v3m3 5h-5v5h5m-3-2.5h.01',
  print: 'M7 9V3h10v6M7 17H4V9h16v8h-3M7 14h10v7H7z',
  down: 'M12 3v12m-5-5 5 5 5-5M4 17v4h16v-4',
  layers: 'm12 3 9 5-9 5-9-5zM3 12l9 5 9-5M3 16l9 5 9-5',
} as const
export function Glyph({ name, size = 18 }: { name: keyof typeof strokes; size?: number }) {
  return (
    <svg
      aria-hidden="true"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {name === 'clock' && <circle cx="12" cy="12" r="9" />}
      <path d={strokes[name]} />
    </svg>
  )
}

export function kickoff(match: Match) {
  if (!match.startsAt || Number.isNaN(Date.parse(match.startsAt))) return null
  const date = new Date(match.startsAt)
  return {
    date: new Intl.DateTimeFormat('id-ID', {
      timeZone: 'Asia/Jakarta',
      weekday: 'long',
      day: 'numeric',
      month: 'long',
      year: 'numeric',
    }).format(date),
    time:
      new Intl.DateTimeFormat('id-ID', {
        timeZone: 'Asia/Jakarta',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }).format(date) + ' WIB',
  }
}

export function Countdown({
  expiresAt,
  onDeadline,
}: {
  expiresAt: string
  onDeadline: () => void
}) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(timer)
  }, [])
  const seconds = Math.max(0, Math.ceil((Date.parse(expiresAt) - now) / 1000))
  useEffect(() => {
    if (seconds === 0) onDeadline()
  }, [seconds, onDeadline])
  return (
    <span className="bx-countdown">
      {String(Math.floor(seconds / 60)).padStart(2, '0')}:{String(seconds % 60).padStart(2, '0')}
    </span>
  )
}

export function EventHero({ match }: { match: Match }) {
  const when = kickoff(match)
  return (
    <section className="bx-hero" aria-labelledby="event-title">
      <div className="bx-hero-content">
        <span className="bx-sport">
          <i />
          Sepak bola
        </span>
        <h1 id="event-title">{match.name}</h1>
        <div className="bx-meta">
          {when && (
            <>
              <span>
                <Glyph name="calendar" />
                {when.date}
              </span>
              <span>
                <Glyph name="clock" />
                {when.time}
              </span>
            </>
          )}
          <span>
            <Glyph name="ticket" />
            Tiket elektronik
          </span>
        </div>
      </div>
    </section>
  )
}

export function Steps({ current }: { current: number }) {
  return (
    <ol className="bx-steps" aria-label="Tahapan pembelian">
      {['Pilih tiket', 'Checkout', 'Pembayaran', 'E-tiket'].map((label, i) => (
        <li
          key={label}
          data-status={i < current ? 'done' : i === current ? 'current' : 'next'}
          aria-current={i === current ? 'step' : undefined}
        >
          <span className="bx-step-num">
            {i < current ? <Glyph name="check" size={14} /> : i + 1}
          </span>
          <span>{label}</span>
        </li>
      ))}
    </ol>
  )
}

export function Stands({
  match,
  selected,
  choose,
  quantity,
  setQuantity,
  stale,
}: {
  match: Match
  selected?: Section
  choose: (s: Section) => void
  quantity: number
  setQuantity: (n: number) => void
  stale: boolean
}) {
  const available = match.sections.reduce((sum, s) => sum + s.available, 0)
  const capacity = match.sections.reduce((sum, s) => sum + s.capacity, 0)
  return (
    <section className="bx-panel bx-stands">
      <div className="bx-panel-head">
        <div>
          <h2>Pilih tribun</h2>
          <p>
            {available} dari {capacity} tiket tersedia
          </p>
        </div>
        <span className={'bx-live' + (stale ? ' bx-stale' : '')}>
          <i />
          {stale ? 'Stok tertunda' : 'Stok live'}
        </span>
      </div>
      <div className="bx-options" aria-label="Pilihan tribun">
        {match.sections.map((section) => (
          <button
            className="bx-option"
            key={section.id}
            type="button"
            aria-pressed={selected?.id === section.id}
            disabled={section.available === 0}
            onClick={() => choose(section)}
          >
            <span className="bx-radio" aria-hidden="true" />
            <span className="bx-option-info">
              <b>Tribun {section.name}</b>
              <small>
                {section.available > 0
                  ? 'Sisa ' + section.available + ' dari ' + section.capacity + ' tiket'
                  : 'Tiket habis'}
              </small>
              <span className="bx-meter" aria-hidden="true">
                <i
                  style={{
                    width:
                      (section.capacity ? (section.available / section.capacity) * 100 : 0) + '%',
                  }}
                />
              </span>
            </span>
            <span className="bx-price">
              <b>{currency(section.priceIdr)}</b>
              <small>per tiket</small>
            </span>
          </button>
        ))}
      </div>
      <div className="bx-quantity">
        <span>
          <b>Jumlah tiket</b>
          <small>Maksimal 4 tiket per pesanan</small>
        </span>
        <div className="bx-counter">
          <button
            aria-label="Kurangi jumlah tiket"
            disabled={quantity <= 1}
            onClick={() => setQuantity(quantity - 1)}
          >
            −
          </button>
          <output aria-live="polite">{quantity}</output>
          <button
            aria-label="Tambah jumlah tiket"
            disabled={!selected || quantity >= Math.min(4, selected.available)}
            onClick={() => setQuantity(quantity + 1)}
          >
            +
          </button>
        </div>
      </div>
    </section>
  )
}

export function Review({
  name,
  onName,
  locked,
  onSubmit,
  formId,
}: {
  name: string
  onName: (s: string) => void
  locked: boolean
  onSubmit: (e: FormEvent<HTMLFormElement>) => void
  formId: string
}) {
  return (
    <section className="bx-panel">
      <div className="bx-panel-head">
        <div>
          <h2>Data pemesan</h2>
          <p>Nama ini akan tercantum pada e-tiket.</p>
        </div>
      </div>
      <form className="bx-form" id={formId} onSubmit={onSubmit}>
        <label htmlFor="buyer-name">
          Nama panggilan <span>(opsional)</span>
        </label>
        <input
          id="buyer-name"
          value={name}
          maxLength={60}
          autoComplete="off"
          placeholder="Contoh: Dika"
          disabled={locked}
          onChange={(e) => onName(e.target.value)}
        />
        <small>Boleh gunakan nama samaran untuk acara demo ini.</small>
      </form>
      <div className="bx-info">
        <Glyph name="clock" />
        <p>
          Setelah pesanan dibuat, tiket ditahan selama <b>5 menit</b> untuk menyelesaikan
          pembayaran.
        </p>
      </div>
    </section>
  )
}

export function PaymentPanel({ hold, onDeadline }: { hold: Hold; onDeadline: () => void }) {
  return (
    <section className="bx-panel">
      <div className="bx-panel-head">
        <div>
          <h2>Pembayaran</h2>
          <p>Selesaikan pembayaran sebelum reservasi berakhir.</p>
        </div>
      </div>
      <div className="bx-deadline">
        <div>
          <Glyph name="clock" />
          <span>Sisa waktu pembayaran</span>
        </div>
        <Countdown expiresAt={hold.expiresAt} onDeadline={onDeadline} />
      </div>
      <div className="bx-method">
        <span className="bx-method-icon">
          <Glyph name="wallet" size={22} />
        </span>
        <div>
          <b>Saldo demo</b>
          <small>Pembayaran simulasi tanpa transaksi uang.</small>
        </div>
        <Glyph name="check" />
      </div>
      <dl className="bx-dl">
        <div>
          <dt>Nomor pesanan</dt>
          <dd className="bx-id">{hold.holdId.slice(0, 8).toUpperCase()}</dd>
        </div>
        {hold.displayName && (
          <div>
            <dt>Pemesan</dt>
            <dd>{hold.displayName}</dd>
          </div>
        )}
      </dl>
    </section>
  )
}
