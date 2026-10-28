import { useTranslation } from 'react-i18next'
import type { KeyboardEvent } from 'react'

/** SettingsTab is one tab of the settings screen. */
export type SettingsTab = 'account' | 'servers' | 'alerts' | 'integrations' | 'system'

const allTabs: SettingsTab[] = ['account', 'servers', 'alerts', 'integrations', 'system']
const storageKey = 'talaia.settingsTab'

/** readSettingsTab returns the tab saved in this browser session, if valid. */
export function readSettingsTab(): SettingsTab | null {
  try {
    const value = sessionStorage.getItem(storageKey)
    return allTabs.includes(value as SettingsTab) ? (value as SettingsTab) : null
  } catch {
    return null
  }
}

/** storeSettingsTab remembers the selected tab for this browser session. */
export function storeSettingsTab(tab: SettingsTab): void {
  try {
    sessionStorage.setItem(storageKey, tab)
  } catch {
    // The tab still works for this render when storage is unavailable.
  }
}

interface SettingsTabsProps {
  tabs: SettingsTab[]
  active: SettingsTab
  onChange: (tab: SettingsTab) => void
}

/** SettingsTabs is the accessible tab bar of the settings screen. */
export function SettingsTabs({ tabs, active, onChange }: SettingsTabsProps) {
  const { t } = useTranslation('settings')
  if (tabs.length === 0) return null

  const move = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
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
    onChange(target)
    requestAnimationFrame(() => {
      document.getElementById(`settings-tab-${target}`)?.focus()
    })
  }

  return (
    <div className="server-tabs" role="tablist" aria-label={t('tabs.label')}>
      {tabs.map((tab, index) => {
        const selected = tab === active
        return (
          <button
            key={tab}
            id={`settings-tab-${tab}`}
            type="button"
            role="tab"
            className={selected ? 'server-tabs__tab server-tabs__tab--on' : 'server-tabs__tab'}
            aria-selected={selected}
            aria-controls={`settings-panel-${tab}`}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(tab)}
            onKeyDown={(event) => move(event, index)}
          >
            {t(`tabs.${tab}`)}
          </button>
        )
      })}
    </div>
  )
}
