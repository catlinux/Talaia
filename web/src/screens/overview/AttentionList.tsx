import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Alert } from '../../api/alerts'
import type { Server } from '../../api/types'
import { Link } from '../../router'
import { formatPercent, formatRelativeTime } from '../fleet/format'
import { AlertMessage } from '../alerts/AlertMessage'
import type { AttentionItem } from './attention'

interface AttentionListProps {
  items: AttentionItem[]
  alerts: Alert[]
  servers: Server[]
  loading?: boolean
}

function relativeTime(
  item: AttentionItem,
  alert: Alert | undefined,
  servers: Server[],
  now: Date,
  language: string,
): string {
  let value = ''
  if (alert !== undefined) {
    value = alert.startedAt
  } else if (item.kind === 'offline') {
    value = servers.find((server) => server.id === item.serverId)?.lastSeen ?? ''
  }
  if (value === '') return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return formatRelativeTime(date, now, language)
}

function contextText(item: AttentionItem, alert: Alert | undefined): string {
  const parts = [alert?.serverName || item.serverName]
  if (alert !== undefined) {
    if (alert.stack !== '') parts.push(alert.stack)
    if (alert.subjectKind === 'container' && alert.subjectName !== '') {
      parts.push(alert.subjectName)
    }
  }
  return parts.join(' › ')
}

function message(item: AttentionItem, language: string) {
  switch (item.kind) {
    case 'alert':
      return null
    case 'offline':
      return { key: 'attention.offline', params: { server: item.serverName } }
    case 'disk':
      return {
        key: 'attention.disk',
        params: {
          server: item.serverName,
          percent: formatPercent(Number(item.params.percent ?? 0), language),
        },
      }
    case 'security':
      return {
        key: 'attention.security',
        params: { count: item.params.count ?? 0, server: item.serverName },
      }
    case 'reboot':
      return { key: 'attention.reboot', params: { server: item.serverName } }
    case 'agent':
      return {
        key: item.params.manual === 1 ? 'attention.agentManual' : 'attention.agent',
        params: { server: item.serverName },
      }
  }
}

/** AttentionList renders the prioritised, actionable list of the Overview. */
export function AttentionList({ items, alerts, servers, loading = false }: AttentionListProps) {
  const { t, i18n } = useTranslation('overview')
  const language = i18n.resolvedLanguage ?? i18n.language
  const [expanded, setExpanded] = useState(false)
  const visible = expanded ? items : items.slice(0, 8)
  const now = new Date()

  return (
    <section className="overview-panel overview-attention" aria-labelledby="attention-title">
      <header className="overview-panel__head">
        <h2 className="overview-panel__title" id="attention-title">
          {t('attention.title')}
        </h2>
        <span className="overview-panel__count">{items.length}</span>
      </header>

      {loading ? (
        <div className="overview-attention__skeleton" aria-busy="true">
          <span className="overview-skeleton__line" />
          <span className="overview-skeleton__line overview-skeleton__line--short" />
          <span className="overview-skeleton__line" />
        </div>
      ) : items.length === 0 ? (
        <div className="overview-attention__empty">
          <span className="overview-attention__empty-mark" aria-hidden="true">
            ✓
          </span>
          <p>{t('attention.empty')}</p>
        </div>
      ) : (
        <>
          <ul className="overview-attention__list">
            {visible.map((item) => {
              const alert =
                item.kind === 'alert' ? alerts.find((entry) => entry.id === item.id) : undefined
              const relative = relativeTime(item, alert, servers, now, language)
              const copy = message(item, language)
              return (
                <li
                  key={item.id}
                  className={`overview-attention__item overview-attention__item--${item.tone}`}
                >
                  <span className="overview-attention__mark" aria-hidden="true">
                    {item.tone === 'crit' ? '!' : item.tone === 'warn' ? '▲' : 'i'}
                  </span>
                  <div className="overview-attention__body">
                    <p className="overview-attention__message">
                      {alert !== undefined ? (
                        <AlertMessage alert={alert} />
                      ) : copy !== null ? (
                        t(copy.key, copy.params)
                      ) : null}
                    </p>
                    <p className="overview-attention__context">
                      {contextText(item, alert)}
                      {relative !== '' ? <span> · {relative}</span> : null}
                    </p>
                  </div>
                  <Link className="ui-btn overview-attention__action" to={item.action.to}>
                    {t(`attention.actions.${actionKey(item)}`)}
                  </Link>
                </li>
              )
            })}
          </ul>
          {items.length > 8 ? (
            <button
              type="button"
              className="overview-attention__more"
              onClick={() => setExpanded((current) => !current)}
            >
              {expanded ? t('attention.showLess') : t('attention.showAll', { count: items.length })}
            </button>
          ) : null}
        </>
      )}
    </section>
  )
}

function actionKey(item: AttentionItem): string {
  switch (item.kind) {
    case 'alert':
      return 'openAlert'
    case 'security':
    case 'reboot':
      return 'viewUpdates'
    case 'agent':
      return 'updateAgent'
    case 'offline':
    case 'disk':
      return 'openServer'
  }
}
