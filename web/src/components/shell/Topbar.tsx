import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertSummary, AlertsApi } from '../../api/alerts'
import type { FleetClient } from '../../api/client'
import type { ContainersApi } from '../../api/containers'
import { AlertBell } from './AlertBell'
import { Breadcrumbs } from './Breadcrumbs'
import { CommandPalette } from './CommandPalette'
import { UserMenu } from './UserMenu'

interface TopbarProps {
  alertSummary: AlertSummary | null
  alertsApi?: AlertsApi
  client?: FleetClient
  containersApi?: ContainersApi
  serverName?: string
  onOpenMenu: () => void
}

function isMac(): boolean {
  const navigatorWithData = navigator as Navigator & { userAgentData?: { platform?: string } }
  const platform = navigatorWithData.userAgentData?.platform ?? navigator.platform ?? ''
  return /mac|iphone|ipad|ipod/i.test(platform)
}

function SearchIcon() {
  return (
    <svg className="shell__search-icon" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
  )
}

/** Topbar holds the drawer button, breadcrumbs, search, alert bell and user menu. */
export function Topbar({
  alertSummary,
  alertsApi,
  client,
  containersApi,
  serverName,
  onOpenMenu,
}: TopbarProps) {
  const { t } = useTranslation('common')
  const [paletteOpen, setPaletteOpen] = useState(false)
  const mac = useMemo(() => isMac(), [])

  return (
    <header className="shell__topbar">
      <button
        type="button"
        className="shell__menu-button"
        aria-label={t('shell.openMenu')}
        onClick={onOpenMenu}
      >
        <span aria-hidden="true">☰</span>
      </button>
      <Breadcrumbs serverName={serverName} />
      <div className="shell__topbar-spacer" />
      <button
        type="button"
        className="shell__search"
        aria-label={t('palette.open')}
        onClick={() => setPaletteOpen(true)}
      >
        <SearchIcon />
        <span className="shell__search-text">{t('palette.search')}</span>
        <kbd className="shell__search-kbd">{mac ? t('palette.cmdK') : t('palette.ctrlK')}</kbd>
      </button>
      <AlertBell summary={alertSummary} api={alertsApi} />
      <UserMenu />
      <CommandPalette
        open={paletteOpen}
        onOpen={() => setPaletteOpen(true)}
        onClose={() => setPaletteOpen(false)}
        client={client}
        containersApi={containersApi}
      />
    </header>
  )
}
