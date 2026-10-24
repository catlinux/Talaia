import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'

import type { Alert } from '../../api/alerts'

const ruleTypes = [
  'cpu_high',
  'mem_high',
  'disk_high',
  'server_offline',
  'container_down',
  'container_unhealthy',
  'check_failed',
  'cert_expiry',
] as const

/** AlertMessage renders the translated summary of an alert, falling back to its rule type. */
export function AlertMessage({ alert }: { alert: Alert }) {
  const { t } = useTranslation('alerts')
  const summaryKey = alert.summary?.key?.replace(/^alerts\./, '') ?? ''
  const rule = summaryKey === '' ? alert.ruleType : summaryKey
  if (!ruleTypes.includes(rule as (typeof ruleTypes)[number])) {
    return <>{alert.ruleType || t('unknownRule', { rule: '—' })}</>
  }
  const params = {
    ...(alert.summary?.params ?? {}),
    server: alert.serverName || alert.serverId || '—',
    subject: alert.subjectName || '—',
  }
  return <>{t(rule, params)}</>
}

/** ruleLabel is the short label of a rule, without its measured values. */
export function ruleLabel(t: TFunction, rule: string): string {
  return t(`ruleLabels.${rule}`, { defaultValue: rule })
}
