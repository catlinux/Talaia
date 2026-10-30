import { useTranslation } from 'react-i18next'
import type { KeyboardEvent } from 'react'

import { Link, useNavigate } from '../../router'

/** SettingsTab is one tab of the settings screen. */
export type SettingsTab = 'account' | 'servers' | 'integrations' | 'system'

/** settingsTabPath returns the canonical address of a settings tab. */
export function settingsTabPath(tab: SettingsTab): string {
  return tab === 'account' ? '/settings' : `/settings/${tab}`
}

/** settingsTabFromPath returns the tab of a settings address, if it has one. */
export function settingsTabFromPath(path: string): SettingsTab | null {
  if (path === '/settings') return 'account'
  if (path === '/settings/servers') return 'servers'
  if (path === '/settings/integrations') return 'integrations'
  if (path === '/settings/system') return 'system'
  return null
}

interface SettingsTabsProps {
  tabs: SettingsTab[]
  active: SettingsTab
}

/** SettingsTabs is the accessible tab bar of the settings screen. */
export function SettingsTabs({ tabs, active }: SettingsTabsProps) {
  const { t } = useTranslation('settings')
  const navigate = useNavigate()
  if (tabs.length === 0) return null

  const move = (event: KeyboardEvent<HTMLAnchorElement>, index: number) => {
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
    const target = tabs[next]!
    navigate(settingsTabPath(target))
    requestAnimationFrame(() => {
      document.getElementById(`settings-tab-${target}`)?.focus()
    })
  }

  return (
    <div className="server-tabs" role="tablist" aria-label={t('tabs.label')}>
      {tabs.map((tab, index) => {
        const selected = tab === active
        return (
          <Link
            key={tab}
            id={`settings-tab-${tab}`}
            to={settingsTabPath(tab)}
            role="tab"
            className={selected ? 'server-tabs__tab server-tabs__tab--on' : 'server-tabs__tab'}
            aria-selected={selected}
            aria-controls={`settings-panel-${tab}`}
            tabIndex={selected ? 0 : -1}
            onKeyDown={(event) => move(event, index)}
          >
            {t(`tabs.${tab}`)}
          </Link>
        )
      })}
    </div>
  )
}
