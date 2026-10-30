import type { AlertsAdminApi } from '../../api/alertsAdmin'
import type { ServerSummary } from '../../api/servers'
import { ChecksCard } from './admin/ChecksCard'

interface ChecksTabProps {
  api: AlertsAdminApi
  servers: ServerSummary[]
}

/** ChecksTab manages the checks that feed alert rules. */
export function ChecksTab({ api, servers }: ChecksTabProps) {
  return <ChecksCard api={api} servers={servers} />
}
