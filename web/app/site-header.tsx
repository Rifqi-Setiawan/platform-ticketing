import './site-header.css'

type Tab = 'office' | 'wallet' | 'lab' | 'admin'
const destinations: { id: Tab; label: string; href: string }[] = [
  { id: 'office', label: 'Loket tiket', href: '/' },
  { id: 'wallet', label: 'Tiket saya', href: '/#tiket-saya' },
  { id: 'lab', label: 'Review lab', href: '/lab' },
  { id: 'admin', label: 'Admin', href: '/admin' },
]

export function SiteHeader({ active }: { active: Tab }) {
  return (
    <header className="tp-mast">
      <div className="tp-mast-inner">
        <a href="/" className="tp-wordmark" aria-label="TribunPass, beranda">
          <span className="mark" aria-hidden="true"><b /><b /><b /></span>
          <strong>Tribun</strong>Pass
        </a>
        <nav className="tp-links" aria-label="Navigasi utama">
          {destinations.map(({ id, label, href }) => (
            <a key={id} href={href} className={id === 'admin' ? 'tp-admin-link' : undefined} aria-current={active === id ? 'page' : undefined}>
              {id === 'admin' && <svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4"><rect x="2" y="2" width="5" height="5" rx="1"/><rect x="9" y="2" width="5" height="5" rx="1"/><rect x="2" y="9" width="5" height="5" rx="1"/><rect x="9" y="9" width="5" height="5" rx="1"/></svg>}
              {label}
            </a>
          ))}
        </nav>
      </div>
    </header>
  )
}
