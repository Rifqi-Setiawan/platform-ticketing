'use client'

import { useEffect, useRef, useState } from 'react'
import {
  api,
  ApiError,
  clock,
  currency,
  describe,
  human,
  openSession,
  readSaved,
  save,
} from './api'
import type { Hold, HoldSnapshot, Match, Payment, Saved, Section } from './api'
import { ReviewLab } from './review-lab'
import { Stadium } from './stadium'

function Brand() {
  return (
    <span className="brand">
      <i className="mark" aria-hidden="true">
        <b />
        <b />
        <b />
      </i>
      Tribun<span>Pass</span>
    </span>
  )
}

function Timer({ expiresAt }: { expiresAt: string }) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 500)
    return () => clearInterval(id)
  }, [])
  const seconds = Math.max(0, Math.ceil((Date.parse(expiresAt) - now) / 1000))
  return (
    <span className="timer">
      {seconds
        ? Math.floor(seconds / 60) + ':' + String(seconds % 60).padStart(2, '0')
        : 'Checking expiry…'}
    </span>
  )
}

function StandOption({
  section,
  picked,
  locked,
  choose,
}: {
  section: Section
  picked: boolean
  locked: boolean
  choose: () => void
}) {
  return (
    <button
      type="button"
      className={'stand-option' + (picked ? ' picked' : '')}
      disabled={locked || section.available === 0}
      aria-pressed={picked}
      onClick={choose}
    >
      <span className="radio" />
      <span className="stand-details">
        <strong>{section.name}</strong>
        <small>
          {section.available
            ? section.available + ' of ' + section.capacity + ' available'
            : 'Sold out'}
        </small>
      </span>
      <span className="stand-price">{currency(section.priceIdr)}</span>
    </button>
  )
}

export default function Home() {
  const [tab, setTab] = useState<'office' | 'lab'>('office')
  const [match, setMatch] = useState<Match | null>(null)
  const [selected, setSelected] = useState('')
  const [hold, setHold] = useState<Hold | null>(null)
  const [saved, setSaved] = useState<Saved | null>(null)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')
  const [fatal, setFatal] = useState('')
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [receipt, setReceipt] = useState<Payment | null>(null)
  const [pollFailed, setPollFailed] = useState(false)
  const locked = useRef(false)
  const selectedStand = match?.sections.find((s) => s.id === selected)

  useEffect(() => {
    let active = true
    async function init() {
      const { sessionId } = await openSession()
      const previous = readSaved()
      let current: Saved =
        previous?.sessionId === sessionId ? previous : { sessionId }
      const { events } = await api<{ events: Match[] }>('events')
      const currentMatch = events[0]
      if (!currentMatch) throw new Error('Session has no events.')
      let currentHold: Hold | null = null
      if (current.holdId) {
        try {
          currentHold = await api<Hold>('holds/' + current.holdId)
        } catch (err) {
          if (!(err instanceof ApiError) || err.status !== 404) throw err
          current = { sessionId }
        }
      }
      if (!active) return
      save(current)
      setSaved(current)
      setMatch(currentMatch)
      setSelected(
        currentHold?.sectionId ||
          current.pending?.sectionId ||
          currentMatch.sections.find((s) => s.available > 0)?.id ||
          '',
      )
      setHold(currentHold)
      setFatal('')
    }
    init().catch((err) => {
      if (active) setFatal(describe(err))
    })
    return () => {
      active = false
    }
  }, [loadAttempt])

  useEffect(() => {
    if (!hold || hold.status !== 'HELD') return
    const id = hold.holdId
    let active = true
    let timer = 0
    async function poll() {
      try {
        const next = await api<Hold>('holds/' + id)
        if (next.status !== 'HELD') {
          const { events } = await api<{ events: Match[] }>('events')
          if (active) setMatch(events[0])
        }
        if (active) {
          setHold(next)
          setPollFailed(false)
        }
      } catch {
        if (active) setPollFailed(true)
      }
      if (active) timer = window.setTimeout(poll, 2000)
    }
    timer = window.setTimeout(poll, 2000)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [hold?.holdId, hold?.status])

  function remember(next: Saved) {
    save(next)
    setSaved(next)
  }
  function pick(section: Section) {
    if (!hold && !saved?.pending && !locked.current) {
      setSelected(section.id)
      setNotice('')
    }
  }
  async function refreshMatch() {
    const { events } = await api<{ events: Match[] }>('events')
    setMatch(events[0])
  }
  async function reserve() {
    if (!saved || !selectedStand || locked.current) return
    locked.current = true
    setBusy('reserve')
    setNotice('')
    try {
      const pending = saved.pending || {
        key: crypto.randomUUID(),
        sectionId: selectedStand.id,
      }
      remember({ sessionId: saved.sessionId, pending })
      const snapshot = await api<HoldSnapshot>(
        'holds',
        { sectionId: pending.sectionId },
        pending.key,
      )
      remember({ sessionId: saved.sessionId, holdId: snapshot.holdId })
      setHold({ ...snapshot, sectionId: pending.sectionId, transitions: [] })
      setHold(await api<Hold>('holds/' + snapshot.holdId))
      await refreshMatch()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'SOLD_OUT') {
        remember({ sessionId: saved.sessionId })
        await refreshMatch().catch(() => {})
      }
      setNotice(describe(err))
    } finally {
      locked.current = false
      setBusy('')
    }
  }
  async function pay() {
    if (!saved || !hold || locked.current) return
    locked.current = true
    setBusy('pay')
    setNotice('')
    try {
      const eventId = saved.paymentId || crypto.randomUUID()
      remember({ ...saved, paymentId: eventId })
      const result = await api<Payment>('demo/payments', {
        eventId,
        holdId: hold.holdId,
      })
      setReceipt(result)
      setHold(await api<Hold>('holds/' + hold.holdId))
      await refreshMatch()
    } catch (err) {
      setNotice(describe(err))
    } finally {
      locked.current = false
      setBusy('')
    }
  }
  async function restart() {
    if (!saved || locked.current) return
    locked.current = true
    setBusy('restart')
    setNotice('')
    try {
      await refreshMatch()
      remember({ sessionId: saved.sessionId })
      setHold(null)
      setReceipt(null)
      setPollFailed(false)
    } catch (err) {
      setNotice(describe(err))
    } finally {
      locked.current = false
      setBusy('')
    }
  }
  const standLocked = Boolean(hold || saved?.pending || busy)
  const total = match?.sections.reduce((n, s) => n + s.capacity, 0) || 0
  return (
    <>
      <header className="site-head">
        <div className="wrap head-inner">
          <Brand />
          <nav aria-label="Main navigation">
            <button
              aria-current={tab === 'office' ? 'page' : undefined}
              onClick={() => setTab('office')}
            >
              Ticket office
            </button>
            <button
              aria-current={tab === 'lab' ? 'page' : undefined}
              onClick={() => setTab('lab')}
            >
              Review lab
            </button>
          </nav>
          <span className="sandbox">
            <i /> Demo sandbox
          </span>
        </div>
      </header>
      <main className="wrap">
        {!match ? (
          <section className="load-box" role={fatal ? 'alert' : 'status'}>
            <h1>
              {fatal
                ? 'Ticket office unavailable'
                : 'Opening the ticket office…'}
            </h1>
            {fatal && (
              <>
                <p>{fatal}</p>
                <button
                  className="primary"
                  onClick={() => {
                    setFatal('')
                    setLoadAttempt((n) => n + 1)
                  }}
                >
                  Retry connection
                </button>
              </>
            )}
          </section>
        ) : tab === 'lab' ? (
          <ReviewLab />
        ) : (
          <>
            <section className="hero">
              <div className="hero-copy">
                <span className="eyebrow light">
                  <i /> Match ticket office
                </span>
                <h1>{match.name}</h1>
                <div className="hero-facts">
                  <span>
                    <b>{match.sections.length}</b> stands
                  </span>
                  <span>
                    <b>{total}</b> tickets
                  </span>
                  <span>
                    <b>5 min</b> hold
                  </span>
                </div>
              </div>
              <Stadium
                sections={match.sections}
                selected={selected}
                locked={standLocked}
                onPick={pick}
              />
            </section>
            <div className="booking-grid">
              <section className="card picker">
                <div className="section-kicker">Match inventory</div>
                <h2>Choose your stand</h2>
                <p className="sub">
                  One ticket per reservation. Availability is confirmed when you
                  reserve.
                </p>
                <div className="stand-list">
                  {match.sections.map((s) => (
                    <StandOption
                      key={s.id}
                      section={s}
                      picked={s.id === selected}
                      locked={standLocked}
                      choose={() => pick(s)}
                    />
                  ))}
                </div>
                <div className="hold-note">
                  <span className="clock-icon" aria-hidden="true" />
                  <div>
                    <b>Five-minute reservation</b>
                    <p>
                      Your place is held while you complete the simulated
                      payment.
                    </p>
                  </div>
                </div>
              </section>
              <section className="ticket" aria-label="Reservation summary">
                <div className="ticket-top">
                  <span className="section-kicker">Box office</span>
                  <span className="ticket-icon" aria-hidden="true" />
                </div>
                <h2>
                  {hold?.status === 'CONFIRMED'
                    ? 'Ticket confirmed'
                    : hold?.status === 'EXPIRED'
                      ? 'Reservation expired'
                      : hold
                        ? 'Ticket on hold'
                        : 'Your reservation'}
                </h2>
                <div className="ticket-body">
                  <div className="summary-line">
                    <span>Stand</span>
                    <strong>{selectedStand?.name || 'Choose a stand'}</strong>
                  </div>
                  <div className="summary-line">
                    <span>Quantity</span>
                    <strong>1 ticket</strong>
                  </div>
                  {hold && (
                    <div className="summary-line">
                      <span>
                        {hold.status === 'HELD' ? 'Time remaining' : 'Status'}
                      </span>
                      <strong>
                        {hold.status === 'HELD' ? (
                          <Timer expiresAt={hold.expiresAt} />
                        ) : (
                          human(hold.status)
                        )}
                      </strong>
                    </div>
                  )}
                </div>
                <div className="ticket-tear" />
                <div className="ticket-total">
                  <span>Ticket total</span>
                  <strong>
                    {selectedStand || hold
                      ? currency(hold?.priceIdr ?? selectedStand!.priceIdr)
                      : '—'}
                  </strong>
                </div>
                {hold?.ticketId && (
                  <div className="ticket-code">
                    <span>Issued ticket</span>
                    <code>{hold.ticketId}</code>
                  </div>
                )}
                {!hold ? (
                  <button
                    className="primary"
                    onClick={reserve}
                    disabled={
                      !!busy ||
                      !selectedStand ||
                      (!saved?.pending && !selectedStand.available)
                    }
                  >
                    {busy === 'reserve'
                      ? 'Reserving…'
                      : saved?.pending
                        ? 'Retry reservation'
                        : 'Reserve ticket'}{' '}
                    <span aria-hidden="true">↗</span>
                  </button>
                ) : (
                  <>
                    {(hold.status === 'HELD' || saved?.paymentId) && (
                      <button
                        className="primary"
                        disabled={!!busy}
                        onClick={pay}
                      >
                        {busy === 'pay'
                          ? 'Sending payment…'
                          : saved?.paymentId
                            ? 'Replay payment'
                            : 'Simulate payment'}{' '}
                        <span aria-hidden="true">↗</span>
                      </button>
                    )}
                    {hold.status !== 'HELD' && (
                      <button
                        className="secondary"
                        disabled={!!busy}
                        onClick={restart}
                      >
                        Start a new reservation
                      </button>
                    )}
                  </>
                )}
                {receipt && (
                  <div className="inline-result" role="status">
                    Payment response <strong>{human(receipt.outcome)}</strong>
                  </div>
                )}
                {pollFailed && (
                  <p className="note" role="status">
                    Connection interrupted. Retrying status…
                  </p>
                )}
                {notice && (
                  <p className="error" role="alert">
                    {notice}
                  </p>
                )}
              </section>
            </div>
            {hold && (
              <section className="card history">
                <div className="section-kicker">Reservation record</div>
                <h2>Activity</h2>
                <p className="muted mono id-line">Hold {hold.holdId}</p>
                <ol className="timeline">
                  {(hold.transitions || []).map((t) => (
                    <li key={t.sequence}>
                      <span className="timeline-dot" />
                      <div>
                        <strong>{human(t.to)}</strong>
                        <small>{human(t.cause)}</small>
                      </div>
                      <time dateTime={t.at}>{clock(t.at)}</time>
                    </li>
                  ))}
                </ol>
              </section>
            )}
          </>
        )}
      </main>
      <footer className="wrap foot">
        <span>
          TribunPass <b>/</b> Tournament ticketing
        </span>
        <span>Simulated payments only</span>
      </footer>
    </>
  )
}
