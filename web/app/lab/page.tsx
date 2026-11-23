import type { Metadata } from 'next'
import { ReviewLab } from '../review-lab'
import { SiteHeader } from '../site-header'

export const metadata: Metadata = { title: 'Review lab | TribunPass' }

export default function LabPage() {
  return <>
    <SiteHeader active="lab" />
    <main className="wrap" style={{ paddingBottom: 56 }}><ReviewLab /></main>
  </>
}
