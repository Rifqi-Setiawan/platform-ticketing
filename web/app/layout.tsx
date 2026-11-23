import type { Metadata } from 'next'
import './styles.css'

export const metadata: Metadata = {
  title: 'TribunPass | Tiket pertandingan',
  description:
    'Pilih tribun, pesan tiket pertandingan, dan simpan e-tiket Nusantara Cup di TribunPass.',
}

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="id">
      <body>{children}</body>
    </html>
  )
}
