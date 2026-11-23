'use client'

import Link from 'next/link'
import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { api, ApiError, currency, openSession } from '../api'
import { SiteHeader } from '../site-header'
import styles from './admin.module.css'

type Status = 'HELD' | 'CONFIRMED' | 'EXPIRED'
type Stand = {
  id: string
  name: string
  capacity: number
  available: number
  held: number
  sold: number
  priceIdr: number
  revenueIdr: number
}
type Order = {
  holdId: string
  displayName: string
  sectionName: string
  quantity: number
  totalIdr: number
  status: Status
  createdAt: string
  expiresAt: string
}
type Overview = {
  eventId: string
  eventName: string
  sections: Stand[]
  recentOrders: Order[]
}

const number = new Intl.NumberFormat('id-ID')
const orderTime = new Intl.DateTimeFormat('id-ID', {
  timeZone: 'Asia/Jakarta',
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
})
const timeOnly = new Intl.DateTimeFormat('id-ID', {
  timeZone: 'Asia/Jakarta',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
})
const statuses: Record<Status, string> = {
  HELD: 'Menunggu bayar',
  CONFIRMED: 'Lunas',
  EXPIRED: 'Kedaluwarsa',
}
const statusStyles: Record<Status, string> = {
  HELD: styles.held,
  CONFIRMED: styles.sold,
  EXPIRED: styles.expired,
}
const icons = {
  stands: 'M3 21V16h5v-5h5V6h8v15z M3 21h18',
  orders: 'M5 3h14v18l-3-2-4 2-4-2-3 2z M8 8h8 M8 12h6',
  arrow: 'M5 12h14 M13 6l6 6-6 6',
  refresh: 'M20 7v4h-4 M20 11a8 8 0 1 0-1.5 6',
  ticket: 'M3 6h18v4a2 2 0 0 0 0 4v4H3v-4a2 2 0 0 0 0-4z M14 8v1m0 2v1m0 2v1',
  clock: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0z M12 7v5l3 2',
  revenue: 'M3 5h16a2 2 0 0 1 2 2v12H5a2 2 0 0 1-2-2z M21 10h-6v4h6',
  edit: 'M4 20l4-1 11-11-3-3L5 16z M14 7l3 3',
} as const

function Icon({
  name,
  size = 18,
}: {
  name: keyof typeof icons
  size?: number
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={icons[name]} />
    </svg>
  )
}

function messageFor(error: unknown) {
  if (error instanceof ApiError) {
    if (error.code === 'CAPACITY_BELOW_ALLOCATED')
      return 'Sebagian tiket sudah dipesan. Periksa kuota terbaru lalu masukkan kapasitas yang lebih besar.'
    if (error.status === 401)
      return 'Sesi demo telah berakhir. Muat ulang halaman untuk membuka sesi baru.'
    if (error.status === 429)
      return 'Terlalu banyak permintaan. Tunggu sebentar lalu coba lagi.'
    if (error.status === 404) return 'Tribun tidak tersedia dalam sesi ini.'
  }
  return 'Data belum dapat dimuat. Periksa koneksi lalu coba lagi.'
}

function Capacity({
  stand,
  refresh,
}: {
  stand: Stand
  refresh: () => Promise<void>
}) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const input = useRef<HTMLInputElement>(null)
  const opener = useRef<HTMLButtonElement>(null)
  const minimum = Math.max(1, stand.sold + stand.held)
  const finish = () => {
    setEditing(false)
    requestAnimationFrame(() => opener.current?.focus())
  }
  useEffect(() => {
    if (editing) {
      input.current?.focus()
      input.current?.select()
    }
  }, [editing])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (saving) return
    const desired = Number(draft)
    if (
      !/^\d+$/.test(draft) ||
      !Number.isSafeInteger(desired) ||
      desired < minimum ||
      desired > 1000
    ) {
      setError(`Masukkan angka bulat antara ${minimum} dan 1.000.`)
      input.current?.focus()
      return
    }
    setSaving(true)
    setError('')
    try {
      await api(
        'admin/sections/' + encodeURIComponent(stand.id) + '/capacity',
        { capacity: desired },
      )
      await refresh()
      setSuccess(`Kapasitas tersimpan: ${number.format(desired)} tiket.`)
      finish()
    } catch (err) {
      setError(
        err instanceof ApiError && err.status < 500
          ? messageFor(err)
          : 'Perubahan belum terkonfirmasi. Periksa kapasitas terbaru sebelum mencoba lagi.',
      )
      await refresh()
    } finally {
      setSaving(false)
    }
  }
  return (
    <section className={styles.stand} aria-label={'Inventori ' + stand.name}>
      <div className={styles.standTop}>
        <div>
          <h3>{stand.name}</h3>
          <p>{currency(stand.priceIdr)} / tiket</p>
        </div>
        <strong>
          {number.format(stand.available)} <small>tersedia</small>
        </strong>
      </div>
      <div className={styles.band} aria-hidden="true">
        {(
          [
            ['sold', stand.sold],
            ['held', stand.held],
            ['available', stand.available],
          ] as const
        ).map(([key, value]) => (
          <span
            key={key}
            className={styles[key + 'Band']}
            style={{
              width:
                (stand.capacity ? (value / stand.capacity) * 100 : 0) + '%',
            }}
          />
        ))}
      </div>
      <ul className={styles.legend}>
        <li>
          <i className={styles.soldBand} />
          {stand.sold} terjual
        </li>
        <li>
          <i className={styles.heldBand} />
          {stand.held} ditahan
        </li>
        <li>
          <i className={styles.availableBand} />
          {stand.available} tersedia
        </li>
      </ul>
      <div className={styles.capacityLine}>
        <span>
          Kapasitas <b>{number.format(stand.capacity)}</b>
        </span>
        {!editing && (
          <button
            type="button"
            ref={opener}
            className={styles.textButton}
            aria-label={'Ubah kapasitas ' + stand.name}
            onClick={() => {
              setDraft(String(stand.capacity))
              setEditing(true)
              setSuccess('')
              setError('')
            }}
          >
            <Icon name="edit" size={14} />
            Ubah
          </button>
        )}
      </div>
      {editing && (
        <form className={styles.form} onSubmit={submit} noValidate>
          <label htmlFor={'capacity-' + stand.id}>Kapasitas {stand.name}</label>
          <div>
            <input
              id={'capacity-' + stand.id}
              type="number"
              min={minimum}
              max={1000}
              step={1}
              inputMode="numeric"
              ref={input}
              value={draft}
              disabled={saving}
              aria-invalid={!!error}
              aria-describedby={'hint-' + stand.id}
              onChange={(e) => {
                setDraft(e.target.value)
                setError('')
              }}
            />
            <button className={styles.saveButton} disabled={saving}>
              {saving ? 'Menyimpan…' : 'Simpan'}
            </button>
            <button
              className={styles.cancelButton}
              type="button"
              disabled={saving}
              onClick={finish}
            >
              Batal
            </button>
          </div>
          <small id={'hint-' + stand.id}>
            Minimal {minimum} tiket · Maksimal 1.000 tiket.
          </small>
          {error && (
            <p role="alert" className={styles.errorText}>
              {error}
            </p>
          )}
        </form>
      )}
      {success && (
        <p role="status" className={styles.success}>
          {success}
        </p>
      )}
    </section>
  )
}

function Orders({ orders }: { orders: Order[] }) {
  const [filter, setFilter] = useState<Status | 'ALL'>('ALL')
  const rows =
    filter === 'ALL' ? orders : orders.filter((o) => o.status === filter)
  return (
    <section
      className={styles.panel}
      id="pesanan"
      aria-labelledby="orders-title"
    >
      <div className={styles.panelHeading}>
        <div>
          <h2 id="orders-title">Pesanan terbaru</h2>
          <p>Maksimal 50 pesanan dari sesi ini.</p>
        </div>
        <label className={styles.filter}>
          Status{' '}
          <select
            aria-label="Filter status pesanan"
            value={filter}
            onChange={(e) => setFilter(e.target.value as typeof filter)}
          >
            <option value="ALL">Semua status</option>
            {(['CONFIRMED', 'HELD', 'EXPIRED'] as const).map((s) => (
              <option key={s} value={s}>
                {statuses[s]}
              </option>
            ))}
          </select>
        </label>
      </div>
      {rows.length ? (
        <table className={styles.table}>
          <thead>
            <tr>
              <th>Pesanan</th>
              <th>Tribun</th>
              <th>Tiket</th>
              <th>Total</th>
              <th>Status</th>
              <th>Waktu (WIB)</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((order) => (
              <tr key={order.holdId}>
                <td className={styles.buyer}>
                  <strong>{order.displayName || 'Pembeli tamu'}</strong>
                  <code title={order.holdId}>
                    #{order.holdId.slice(0, 8).toUpperCase()}
                  </code>
                </td>
                <td data-label="Tribun">{order.sectionName}</td>
                <td data-label="Tiket">{order.quantity}</td>
                <td data-label="Total">{currency(order.totalIdr)}</td>
                <td data-label="Status">
                  <span
                    className={styles.tag + ' ' + statusStyles[order.status]}
                  >
                    {statuses[order.status]}
                  </span>
                </td>
                <td data-label="Waktu (WIB)">
                  <time dateTime={order.createdAt}>
                    {orderTime.format(new Date(order.createdAt))}
                  </time>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className={styles.empty}>
          <span className={styles.emptyIcon}>
            <Icon name="orders" size={23} />
          </span>
          <strong>
            {orders.length
              ? 'Belum ada pesanan dengan status ini'
              : 'Belum ada pesanan'}
          </strong>
          {orders.length ? (
            <button
              type="button"
              className={styles.textButton}
              onClick={() => setFilter('ALL')}
            >
              Tampilkan semua status
            </button>
          ) : (
            <Link href="/">
              Buka loket tiket <Icon name="arrow" size={15} />
            </Link>
          )}
        </div>
      )}
    </section>
  )
}

export default function AdminDashboard() {
  const [data, setData] = useState<Overview | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [updated, setUpdated] = useState('')
  const version = useRef(0)
  const reading = useRef(false)
  const load = useCallback(async (initialize = false, background = false) => {
    if (background && reading.current) return
    const seq = ++version.current
    reading.current = true
    if (!background) setBusy(true)
    try {
      if (initialize) await openSession()
      const next = await api<Overview>('admin/overview')
      if (seq === version.current) {
        setData(next)
        setError('')
        setUpdated(timeOnly.format(new Date()))
      }
    } catch (err) {
      if (seq === version.current) setError(messageFor(err))
    } finally {
      if (seq === version.current) {
        setBusy(false)
        reading.current = false
      }
    }
  }, [])
  useEffect(() => {
    void load(true)
    const updateVisible = () => {
      if (document.visibilityState === 'visible') void load(false, true)
    }
    const timer = window.setInterval(updateVisible, 15000)
    window.addEventListener('focus', updateVisible)
    return () => {
      version.current++
      clearInterval(timer)
      window.removeEventListener('focus', updateVisible)
    }
  }, [load])
  const totals = data?.sections.reduce(
    (v, s) => ({
      available: v.available + s.available,
      sold: v.sold + s.sold,
      held: v.held + s.held,
      revenue: v.revenue + s.revenueIdr,
      capacity: v.capacity + s.capacity,
    }),
    { available: 0, sold: 0, held: 0, revenue: 0, capacity: 0 },
  )
  return (
    <div className={styles.shell} lang="id">
      <SiteHeader active="admin" />
      <main className={styles.main} id="ringkasan">
        <div className={styles.content}>
          <div className={styles.titleRow}>
            <div>
              <p className={styles.match}>
                {data?.eventName || 'Loket turnamen'}
              </p>
              <h1>Ringkasan penjualan</h1>
              <p className={styles.scope}>
                Data dari sesi demo di browser ini.
              </p>
            </div>
            <button
              type="button"
              className={styles.refresh}
              disabled={busy}
              onClick={() => void load(!data)}
            >
              <Icon name="refresh" size={16} />
              {busy ? 'Memuat…' : 'Perbarui'}
            </button>
          </div>
          {error && (
            <p className={styles.banner} role="alert">
              {error}
              {data && ' Angka terakhir tetap ditampilkan.'}
            </p>
          )}
          {!data || !totals ? (
            <div className={styles.loading} role="status">
              {busy
                ? 'Memuat data dari loket…'
                : 'Data penjualan belum tersedia.'}
            </div>
          ) : (
            <>
              <section
                className={styles.metrics}
                aria-label="Indikator penjualan"
              >
                <div className={styles.revenue}>
                  <span>
                    <Icon name="revenue" />
                    Pendapatan simulasi
                  </span>
                  <strong>{currency(totals.revenue)}</strong>
                  <small>Dari tiket berstatus lunas</small>
                </div>
                <div>
                  <span>
                    <Icon name="ticket" />
                    Tiket terjual
                  </span>
                  <strong>{number.format(totals.sold)}</strong>
                  <small>Dari {number.format(totals.capacity)} tiket</small>
                </div>
                <div>
                  <span>
                    <Icon name="clock" />
                    Ditahan
                  </span>
                  <strong>{number.format(totals.held)}</strong>
                  <small>Menunggu pembayaran</small>
                </div>
                <div>
                  <span>
                    <Icon name="stands" />
                    Tersedia
                  </span>
                  <strong>{number.format(totals.available)}</strong>
                  <small>Siap dipesan di loket</small>
                </div>
              </section>
              <section
                className={styles.panel}
                id="inventori"
                aria-labelledby="stands-title"
              >
                <div className={styles.panelHeading}>
                  <div>
                    <h2 id="stands-title">Inventori tribun</h2>
                    <p>Kuota, harga, dan ketersediaan tiket per tribun.</p>
                  </div>
                </div>
                <div className={styles.stands}>
                  {data.sections.map((s) => (
                    <Capacity key={s.id} stand={s} refresh={() => load()} />
                  ))}
                </div>
              </section>
              <Orders orders={data.recentOrders || []} />
              <div className={styles.stamp}>
                Pembaruan terakhir {updated} WIB
              </div>
            </>
          )}
        </div>
      </main>
    </div>
  )
}
