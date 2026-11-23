'use client'

import { useEffect, useRef, useState } from 'react'
import { api, describe, human } from './api'
import type { Scenario, ScenarioResult } from './api'

const scenarios: {
  id: Scenario
  title: string
  detail: string
  check: string
}[] = [
  {
    id: 'last-ticket',
    title: 'The last ticket',
    detail: 'Two buyers reserve the same remaining ticket at the same time.',
    check:
      'One buyer receives the hold. The other receives a sold-out response.',
  },
  {
    id: 'duplicate-payment',
    title: 'Payment replay',
    detail: 'The same successful payment callback is delivered twice.',
    check: 'Both deliveries resolve to the same ticket ID.',
  },
  {
    id: 'late-payment',
    title: 'Past the deadline',
    detail: 'A payment arrives for a reservation whose deadline has passed.',
    check:
      'The payment is recorded as late and the ticket returns to inventory.',
  },
]

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
        <div className="section-kicker">Engineering sandbox</div>
        <h1>
          Review lab<span>.</span>
        </h1>
        <p>
          Run a booking scenario against the Go API. Each run starts with its
          own ticket inventory.
        </p>
      </section>
      <div className="lab-grid">
        <section className="card lab-picker">
          <h2>Choose a scenario</h2>
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
            {busy ? 'Running scenario…' : result ? 'Run again' : 'Run scenario'}{' '}
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
            <div className="section-kicker">Execution log</div>
            <span className={'run-state' + (result ? ' done' : '')}>
              {busy ? 'Running' : result ? 'Completed' : 'Ready'}
            </span>
          </div>
          <h2>{result ? current.title : 'Scenario results'}</h2>
          {!result ? (
            <div className="expected">
              <span className="terminal-icon" aria-hidden="true">
                ›_
              </span>
              <strong>
                {busy ? 'Waiting for API results…' : 'Expected result'}
              </strong>
              <p>{current.check}</p>
            </div>
          ) : (
            <>
              <div className="result-stat">
                <strong>{result.available}</strong>
                <span>of {result.capacity} tickets available after run</span>
              </div>
              <ol className="timeline">
                {result.steps.map((step, i) => (
                  <li key={i}>
                    <span className="timeline-dot" />
                    <div>
                      <strong>{step.label}</strong>
                      <small>{human(step.outcome)}</small>
                      {step.holdId && <code>Hold {step.holdId}</code>}
                      {step.eventId && <code>Event {step.eventId}</code>}
                      {step.ticketId && <code>Ticket {step.ticketId}</code>}
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
