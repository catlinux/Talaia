import type { AlertsAdminApi } from '../../api/alertsAdmin'
import { ChannelsCard } from './admin/ChannelsCard'
import { PrivateTargetsCard } from './admin/PrivateTargetsCard'

interface ChannelsTabProps {
  api: AlertsAdminApi
}

/** ChannelsTab manages notification channels and the private-target policy. */
export function ChannelsTab({ api }: ChannelsTabProps) {
  return (
    <>
      <ChannelsCard api={api} />
      <PrivateTargetsCard api={api} />
    </>
  )
}
