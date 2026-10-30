import type { AlertsAdminApi } from '../../api/alertsAdmin'
import type { Group } from '../../api/groups'
import type { ServerSummary } from '../../api/servers'
import { useRoute } from '../../router'
import { RulesCard } from './admin/RulesCard'

interface RulesTabProps {
  api: AlertsAdminApi
  servers: ServerSummary[]
  groups: Group[]
}

/** RulesTab manages the alert rules from the alerts page. */
export function RulesTab({ api, servers, groups }: RulesTabProps) {
  const { query } = useRoute()
  return <RulesCard api={api} servers={servers} groups={groups} initialRuleId={query.get('rule')} />
}
