import type { KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { AptSummary } from '../../api/types'
import { Link, useNavigate } from '../../router'
import type { ServerTab } from './OverviewTab'
import { tabPath } from './serverTabs'

interface ServerTabsProps {
  serverId: string
  tabs: ServerTab[]
  activeTab: ServerTab
  apt?: AptSummary
  failedServices: number | null
}

/** ServerTabs is the URL-backed tab bar of a server page. */
export function ServerTabs({ serverId, tabs, activeTab, apt, failedServices }: ServerTabsProps) {
  const { t } = useTranslation('server')
  const navigate = useNavigate()

  const moveTab = (event: KeyboardEvent<HTMLAnchorElement>, index: number) => {
    let next: number
    switch (event.key) {
      case 'ArrowLeft':
        next = (index - 1 + tabs.length) % tabs.length
        break
      case 'ArrowRight':
        next = (index + 1) % tabs.length
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = tabs.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    const tab = tabs[next]!
    navigate(tabPath(serverId, tab))
    requestAnimationFrame(() => {
      document.getElementById(`server-tab-${tab}`)?.focus()
    })
  }

  return (
    <div className="server-tabs" role="tablist" aria-label={t('tabs.label')}>
      {tabs.map((tab, index) => {
        const selected = activeTab === tab
        const label = t(`tabs.${tab}`)
        const updatesBadge = tab === 'updates' && apt ? `${apt.security}/${apt.upgradable}` : ''
        const failedBadge =
          tab === 'services' && failedServices !== null && failedServices > 0
            ? t('tabs.failedBadge', { count: failedServices })
            : ''
        const badge = updatesBadge || failedBadge
        const alert = tab === 'updates' && (apt?.security ?? 0) > 0
        const badgeID = badge === '' ? undefined : `server-tab-${tab}-badge`
        return (
          <Link
            key={tab}
            id={`server-tab-${tab}`}
            to={tabPath(serverId, tab)}
            role="tab"
            className={selected ? 'server-tabs__tab server-tabs__tab--on' : 'server-tabs__tab'}
            aria-label={label}
            aria-selected={selected}
            aria-controls={`server-panel-${tab}`}
            aria-describedby={badgeID}
            tabIndex={selected ? 0 : -1}
            onKeyDown={(event) => moveTab(event, index)}
          >
            <span>{label}</span>
            {badge !== '' ? (
              <span
                id={badgeID}
                className={
                  alert ? 'server-tabs__badge server-tabs__badge--alert' : 'server-tabs__badge'
                }
              >
                {badge}
              </span>
            ) : null}
          </Link>
        )
      })}
    </div>
  )
}
