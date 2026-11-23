import type { Metadata } from 'next'
import './styles.css'

export const metadata: Metadata = {
  title: 'TribunPass | Matchday tickets',
  description:
    'Reserve a tournament ticket and inspect how TribunPass handles competing buyers, payment retries and expiry.',
}

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
