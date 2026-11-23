'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError, currency, describe, openSession, readSaved, save } from './api'
import type { Hold, HoldSnapshot, Match, Payment, Saved, Section } from './api'
import { EventHero, Glyph, PaymentPanel, Review, Stands, Steps } from './booking-ui'
import { TicketBundle, Wallet } from './tickets'
import { SiteHeader } from './site-header'
import './buyer.css'

export default function Home() {
  const [match, setMatch] = useState<Match | null>(null)
  const [saved, setSaved] = useState<Saved | null>(null)
  const [selected, setSelected] = useState('')
  const [quantity, setQuantity] = useState(1)
  const [name, setName] = useState('')
  const [review, setReview] = useState(false)
  const [wallet, setWallet] = useState(false)
  const [hold, setHold] = useState<Hold | null>(null)
  const [notice, setNotice] = useState('')
  const [fatal, setFatal] = useState('')
  const [retry, setRetry] = useState(0)
  const [stale, setStale] = useState(false)
  const [busy, setBusy] = useState(false)
  const [deadlinePassed, setDeadlinePassed] = useState(false)
  const locked = useRef(false)
  const sequence = useRef(0)
  const flow = useRef<HTMLDivElement>(null)
  const chosen = match?.sections.find((s) => s.id === (hold?.sectionId || selected))
  const step = hold?.status === 'CONFIRMED' ? 3 : hold ? 2 : review ? 1 : 0
  const qty = hold?.quantity ?? quantity
  const price = hold?.priceIdr ?? chosen?.priceIdr ?? 0
  const total = hold?.totalIdr ?? price * qty
  const outOfStock = !chosen || chosen.available < qty
  const atDeadline = useCallback(() => setDeadlinePassed(true), [])

  const persist = useCallback((next: Saved) => {
    save(next)
    setSaved(next)
  }, [])

  const refreshStock = useCallback(async () => {
    const serial = ++sequence.current
    const { events } = await api<{ events: Match[] }>('events')
    if (!events[0]) throw new Error('No match in session')
    if (serial === sequence.current) {
      setMatch(events[0])
      setStale(false)
    }
    return events[0]
  }, [])

  useEffect(() => {
    const sync = () => setWallet(window.location.hash === '#tiket-saya')
    sync()
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])

  useEffect(() => {
    let alive = true
    async function init() {
      const { sessionId } = await openSession()
      const previous = readSaved()
      let current: Saved = previous?.sessionId === sessionId ? previous : { sessionId }
      const event = await refreshStock()
      let existing: Hold | null = null
      if (current.holdId) {
        try {
          existing = await api<Hold>('holds/' + current.holdId)
        } catch (err) {
          if (!(err instanceof ApiError) || err.status !== 404) throw err
          current = { sessionId }
        }
      }
      if (!alive) return
      persist(current)
      setSelected(
        existing?.sectionId ||
          current.pending?.sectionId ||
          event.sections.find((s) => s.available > 0)?.id ||
          event.sections[0]?.id ||
          '',
      )
      setQuantity(existing?.quantity ?? current.pending?.quantity ?? 1)
      setName(existing?.displayName ?? current.pending?.displayName ?? '')
      setReview(!!current.pending)
      setHold(existing)
      setFatal('')
    }
    init().catch((e) => {
      if (alive) setFatal(describe(e))
    })
    return () => {
      alive = false
    }
  }, [retry, refreshStock, persist])

  useEffect(() => {
    if (!saved?.sessionId) return
    const refresh = () => {
      if (document.visibilityState === 'visible') refreshStock().catch(() => setStale(true))
    }
    const timer = window.setInterval(refresh, 6000)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [saved?.sessionId, refreshStock])

  useEffect(() => {
    setDeadlinePassed(false)
    if (!hold || hold.status !== 'HELD') return
    let alive = true
    let timer: number
    const id = hold.holdId
    async function poll() {
      try {
        const result = await api<Hold>('holds/' + id)
        if (!alive) return
        setHold((previous) =>
          previous?.holdId === id && previous.status === 'HELD' ? result : previous,
        )
        if (result.status !== 'HELD') await refreshStock()
      } catch {
        if (alive) setStale(true)
      }
      if (alive) timer = window.setTimeout(poll, 2400)
    }
    timer = window.setTimeout(poll, 2400)
    return () => {
      alive = false
      clearTimeout(timer)
    }
  }, [hold?.holdId, hold?.status, refreshStock])

  function focusFlow() {
    requestAnimationFrame(() => {
      flow.current?.scrollIntoView({ block: 'start', behavior: 'instant' })
      flow.current?.focus({ preventScroll: true })
    })
  }
  function choose(section: Section) {
    setSelected(section.id)
    setQuantity((q) => Math.min(q, section.available))
    setNotice('')
  }
  function beginReview() {
    if (outOfStock || busy) return
    setReview(true)
    setNotice('')
    focusFlow()
  }
  async function reserve() {
    if (!saved || !chosen || locked.current) return
    locked.current = true
    setBusy(true)
    setNotice('')
    const pending = saved.pending || {
      key: crypto.randomUUID(),
      sectionId: chosen.id,
      quantity,
      displayName: name.trim(),
    }
    try {
      persist({ ...saved, pending })
      const snapshot = await api<HoldSnapshot>(
        'holds',
        {
          sectionId: pending.sectionId,
          quantity: pending.quantity ?? 1,
          displayName: pending.displayName ?? '',
        },
        pending.key,
      )
      persist({ sessionId: saved.sessionId, holdId: snapshot.holdId })
      setHold({ ...snapshot, sectionId: pending.sectionId, transitions: [] })
      setHold(await api<Hold>('holds/' + snapshot.holdId))
      await refreshStock().catch(() => setStale(true))
      focusFlow()
    } catch (e) {
      if (
        e instanceof ApiError &&
        (e.code === 'SOLD_OUT' || e.status === 400 || e.status === 404)
      ) {
        persist({ sessionId: saved.sessionId })
        setReview(false)
        await refreshStock().catch(() => setStale(true))
      }
      setNotice(describe(e))
    } finally {
      locked.current = false
      setBusy(false)
    }
  }
  async function pay() {
    if (!hold || !saved || locked.current) return
    locked.current = true
    setBusy(true)
    setNotice('')
    try {
      const eventId = saved.paymentId || crypto.randomUUID()
      persist({ ...saved, paymentId: eventId })
      await api<Payment>('demo/payments', { holdId: hold.holdId, eventId })
      const result = await api<Hold>('holds/' + hold.holdId)
      setHold(result)
      await refreshStock().catch(() => setStale(true))
      focusFlow()
    } catch (e) {
      setNotice(describe(e))
    } finally {
      locked.current = false
      setBusy(false)
    }
  }
  function restart() {
    if (!saved) return
    persist({ sessionId: saved.sessionId })
    setHold(null)
    setName('')
    setQuantity(1)
    setReview(false)
    setNotice('')
    setSelected(match?.sections.find((s) => s.available > 0)?.id || match?.sections[0]?.id || '')
    refreshStock().catch(() => setStale(true))
    focusFlow()
  }
  function resume(order: Hold) {
    if (!saved) return
    persist({
      ...saved,
      holdId: order.holdId,
      paymentId: saved.holdId === order.holdId ? saved.paymentId : undefined,
      pending: undefined,
    })
    setHold(order)
    setSelected(order.sectionId)
    setName(order.displayName || '')
    setQuantity(order.quantity ?? 1)
    setWallet(false)
    window.history.replaceState(null, '', '/')
    focusFlow()
  }
  return (
    <>
      <SiteHeader active={wallet ? 'wallet' : 'office'} />
      <main className="bx-main" lang="id">
        {!match || !saved ? (
          <section className="bx-panel bx-loading" role={fatal ? 'alert' : 'status'}>
            <h1>{fatal ? 'Loket belum terhubung' : 'Memuat pertandingan…'}</h1>
            {fatal && (
              <>
                <p>{fatal}</p>
                <button
                  className="bx-primary"
                  onClick={() => {
                    setFatal('')
                    setRetry((n) => n + 1)
                  }}
                >
                  Coba lagi
                </button>
              </>
            )}
          </section>
        ) : wallet ? (
          <Wallet match={match} resume={resume} />
        ) : (
          <>
            {step === 0 ? (
              <EventHero match={match} />
            ) : (
              <div className="bx-matchline">
                <Glyph name="ticket" />
                <strong>{match.name}</strong>
                <span>Loket pertandingan</span>
              </div>
            )}
            <div ref={flow} tabIndex={-1} className="bx-flow">
              <Steps current={step} />
            </div>
            {hold?.status === 'CONFIRMED' ? (
              <section className="bx-success">
                <div className="bx-success-title">
                  <span className="bx-check">
                    <Glyph name="check" size={24} />
                  </span>
                  <div>
                    <h2>Tiket berhasil terbit</h2>
                    <p>{qty} e-tiket tersedia di menu Tiket saya.</p>
                  </div>
                </div>
                <TicketBundle match={match} hold={hold} />
                <button className="bx-text-button bx-after" onClick={restart}>
                  Pesan tiket lagi <Glyph name="arrow" />
                </button>
              </section>
            ) : hold?.status === 'EXPIRED' ? (
              <section className="bx-panel bx-expired">
                <Glyph name="clock" size={32} />
                <h2>Waktu pembayaran habis</h2>
                <p>
                  Pesanan #{hold.holdId.slice(0, 8).toUpperCase()} kedaluwarsa. Kuotanya sudah
                  dikembalikan ke tribun.
                </p>
                <button className="bx-primary" onClick={restart}>
                  Pilih tiket lagi <Glyph name="arrow" />
                </button>
              </section>
            ) : (
              <div className="bx-columns">
                {hold ? (
                  <PaymentPanel hold={hold} onDeadline={atDeadline} />
                ) : review ? (
                  <Review
                    name={name}
                    onName={setName}
                    locked={!!saved.pending || busy}
                    formId="buyer-review"
                    onSubmit={(e) => {
                      e.preventDefault()
                      reserve()
                    }}
                  />
                ) : (
                  <Stands
                    match={match}
                    selected={chosen}
                    choose={choose}
                    quantity={quantity}
                    setQuantity={setQuantity}
                    stale={stale}
                  />
                )}
                <aside className="bx-summary" aria-labelledby="summary-title">
                  <div className="bx-summary-head">
                    <h2 id="summary-title">Ringkasan pesanan</h2>
                    <Glyph name="ticket" size={22} />
                  </div>
                  <p className="bx-summary-event">{match.name}</p>
                  <dl className="bx-dl">
                    <div>
                      <dt>Kategori</dt>
                      <dd>{chosen ? 'Tribun ' + chosen.name : '—'}</dd>
                    </div>
                    <div>
                      <dt>Harga satuan</dt>
                      <dd>{currency(price)}</dd>
                    </div>
                    <div>
                      <dt>Jumlah</dt>
                      <dd>{qty} tiket</dd>
                    </div>
                  </dl>
                  <div className="bx-total">
                    <span>Total pembayaran</span>
                    <b>{currency(total)}</b>
                  </div>
                  {notice && (
                    <p className="bx-alert" role="alert">
                      {notice}
                    </p>
                  )}
                  {hold ? (
                    <button className="bx-primary" onClick={pay} disabled={busy || deadlinePassed}>
                      {busy
                        ? 'Memproses pembayaran…'
                        : deadlinePassed
                          ? 'Memeriksa kedaluwarsa…'
                          : saved.paymentId
                            ? 'Coba lagi pembayaran'
                            : 'Bayar simulasi'}
                      <Glyph name="arrow" />
                    </button>
                  ) : review ? (
                    <>
                      <button
                        type="submit"
                        form="buyer-review"
                        className="bx-primary"
                        disabled={busy || (outOfStock && !saved.pending)}
                      >
                        {busy
                          ? 'Membuat pesanan…'
                          : saved.pending
                            ? 'Coba lagi pesanan'
                            : 'Pesan dan lanjut bayar'}
                        <Glyph name="arrow" />
                      </button>
                      {!saved.pending && (
                        <button
                          className="bx-text-button bx-edit"
                          onClick={() => {
                            setReview(false)
                            setNotice('')
                            focusFlow()
                          }}
                        >
                          <Glyph name="back" size={14} /> Ubah pilihan tiket
                        </button>
                      )}
                    </>
                  ) : (
                    <button
                      type="button"
                      className="bx-primary"
                      disabled={busy || outOfStock}
                      onClick={beginReview}
                    >
                      Lanjut ke checkout <Glyph name="arrow" />
                    </button>
                  )}
                  {!hold && !review && (
                    <p className="bx-summary-note">
                      <Glyph name="clock" size={14} />
                      Masa bayar 5 menit setelah checkout.
                    </p>
                  )}
                  {!hold && outOfStock && (
                    <p className="bx-alert" role="status">
                      Kuota tribun tidak mencukupi untuk jumlah ini.
                    </p>
                  )}
                </aside>
              </div>
            )}
          </>
        )}
      </main>
      <footer className="bx-footer">
        <span>
          <b>TribunPass</b> / Loket pertandingan
        </span>
        <span>Acara dan pembayaran simulasi</span>
      </footer>
    </>
  )
}
