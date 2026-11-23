import type { Metadata } from 'next'

export const metadata: Metadata = {
  title: 'Dashboard admin | TribunPass',
  description:
    'Pantau kuota tribun dan pesanan tiket dalam sesi demo TribunPass.',
}

export default function AdminLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return children
}
