'use client'

import { useEffect, useRef, useState } from 'react'
import { api, describe, openSession } from './api'
import type { Scenario, ScenarioResult } from './api'

const scenarios: {
  id: Scenario
  title: string
  detail: string
  check: string
}[] = [
  {
    id: 'last-ticket',
    title: 'Tiket terakhir',
    detail: 'Dua pembeli memesan satu tiket tersisa secara bersamaan.',
    check:
      'Satu pembeli mendapat reservasi. Pembeli lain mendapat respons stok habis.',
  },
  {
    id: 'duplicate-payment',
    title: 'Pembayaran berulang',
    detail: 'Callback pembayaran yang sama dikirim dua kali.',
    check: 'Kedua pengiriman menghasilkan ID tiket yang sama.',
  },
  {
    id: 'late-payment',
    title: 'Lewat tenggat',
    detail: 'Pembayaran tiba setelah batas waktu reservasi.',
    check:
      'Pembayaran tercatat terlambat dan kuota kembali tersedia.',
  },
]

const stepLabels: Record<string, string> = {
  'Buyer A requested the final ticket': 'Pembeli A memesan tiket terakhir',
  'Buyer B requested the final ticket': 'Pembeli B memesan tiket terakhir',
  'Reserve the ticket': 'Buat reservasi',
  'Start with a hold past its deadline': 'Siapkan reservasi yang melewati tenggat',
  'Deliver the payment callback': 'Kirim callback pembayaran',
  'Redeliver the same event': 'Kirim ulang event yang sama',
}

export function ReviewLab() {
  const [scenario, setScenario] = useState<Scenario>('last-ticket')
  const [result, setResult] = useState<ScenarioResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const running = useRef(false)
  const log = useRef<HTMLElement>(null)
  const current = scenarios.find((s) => s.id === scenario)!
  useEffect(() => {
    if (result && window.matchMedia('(max-width: 620px)').matches) {
      log.current?.scrollIntoView({ block: 'start', behavior: 'instant' })
    }
  }, [result])
  async function run() {
    if (running.current) return
    running.current = true
    setBusy(true)
    setError('')
    setResult(null)
    try {
      await openSession()
      setResult(await api<ScenarioResult>('demo/scenarios', { scenario }))
    } catch (err) {
      setError(describe(err))
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  return (
    <>
      <section className="lab-head">
        <h1>
          Review lab<span>.</span>
        </h1>
        <p>
          Uji perebutan tiket, pengiriman ulang pembayaran, dan batas waktu reservasi. Setiap skenario memakai kuota tersendiri.
        </p>
      </section>
      <div className="lab-grid">
        <section className="card lab-picker">
          <h2>Pilih skenario</h2>
          <div className="scenario-list">
            {scenarios.map((s) => (
              <button
                type="button"
                className={
                  'scenario-opt' + (s.id === scenario ? ' picked' : '')
                }
                aria-pressed={s.id === scenario}
                key={s.id}
                disabled={busy}
                onClick={() => {
                  setScenario(s.id)
                  setError('')
                  setResult(null)
                }}
              >
                <span className="radio" />
                <span>
                  <b>{s.title}</b>
                  <small>{s.detail}</small>
                </span>
              </button>
            ))}
          </div>
          <button className="primary" onClick={run} disabled={busy}>
            {busy ? 'Menjalankan…' : result ? 'Jalankan lagi' : 'Jalankan skenario'}{' '}
            <span aria-hidden="true">↗</span>
          </button>
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
        </section>
        <section className="card results" aria-live="polite" ref={log}>
          <div className="result-head">
            <div className="section-kicker">Riwayat eksekusi</div>
            <span className={'run-state' + (result ? ' done' : '')}>
              {busy ? 'Berjalan' : result ? 'Selesai' : 'Siap'}
            </span>
          </div>
          <h2>{result ? current.title : 'Hasil skenario'}</h2>
          {!result ? (
            <div className="expected">
              <span className="terminal-icon" aria-hidden="true">
                ›_
              </span>
              <strong>
                {busy ? 'Menunggu hasil API…' : 'Hasil yang diharapkan'}
              </strong>
              <p>{current.check}</p>
            </div>
          ) : (
            <>
              <div className="result-stat">
                <strong>{result.available}</strong>
                <span>dari {result.capacity} tiket tersedia setelah pengujian</span>
              </div>
              <ol className="timeline">
                {result.steps.map((step, i) => (
                  <li key={i}>
                    <span className="timeline-dot" />
                    <div>
                      <strong>{stepLabels[step.label] || step.label}</strong>
                      <small>{step.outcome}</small>
                      {step.holdId && <code>Reservasi {step.holdId}</code>}
                      {step.eventId && <code>Event {step.eventId}</code>}
                      {step.ticketId && <code>Tiket {step.ticketId}</code>}
                    </div>
                  </li>
                ))}
              </ol>
            </>
          )}
        </section>
      </div>
    </>
  )
}
