import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { AptApi } from './api/apt'
import type { AuditApi } from './api/audit'
import type { FleetClient } from './api/client'
import type { ContainersApi } from './api/containers'
import type { JournalApi } from './api/journal'
import type { ServicesApi } from './api/services'
import type { SystemApi } from './api/system'
import type { Server } from './api/types'
import { About } from './screens/About'
import { Audit } from './screens/Audit'
import { Fleet } from './screens/Fleet'
import { ServerScreen } from './screens/ServerScreen'
import { Settings } from './screens/Settings'
import { useAuth } from './auth/AuthGate'
import { usePermissions } from './auth/permissions'
import { AuditIcon, FleetIcon, GearIcon, InfoIcon, SignOutIcon } from './components/icons'
import { LanguageSelector } from './components/LanguageSelector'
import { Logo } from './components/Logo'
import { ThemeToggle } from './theme/ThemeToggle'

type Screen = 'fleet' | 'settings' | 'audit' | 'about'

interface AppProps {
  /** Data source of the fleet screen. Tests inject their own client. */
  client?: FleetClient
  /** Container API of the server screen. Tests inject their own. */
  containersApi?: ContainersApi
  /** Apt API of the server screen. Tests inject their own. */
  aptApi?: AptApi
  /** Services API of the server screen. Tests inject their own. */
  servicesApi?: ServicesApi
  /** Journal API of the server screen. Tests inject their own. */
  journalApi?: JournalApi
  /** Version and update API of the settings screen. Tests inject their own. */
  systemApi?: SystemApi
  /** Audit API of the audit screen. Tests inject their own. */
  auditApi?: AuditApi
}

/** initials returns up to two letters to draw the avatar of the signed-in user. */
function initials(username: string): string {
  return username.trim().slice(0, 2).toUpperCase()
}

export function App({
  client,
  containersApi,
  aptApi,
  servicesApi,
  journalApi,
  systemApi,
  auditApi,
}: AppProps) {
  const { t } = useTranslation(['common', 'auth'])
  const auth = useAuth()
  const { can } = usePermissions()
  const canAudit = can('audit.view')
  const [screen, setScreen] = useState<Screen>('fleet')
  // The server whose containers are open, if any; it replaces the fleet screen.
  const [openServer, setOpenServer] = useState<Server | null>(null)

  return (
    <div className="shell">
      <aside className="side">
        <div className="side__brand">
          <Logo className="side__logo" />
          <span className="side__name">{t('appName')}</span>
        </div>

        <nav className="side__nav" aria-label={t('nav.label')}>
          <button
            type="button"
            className={screen === 'fleet' ? 'side__link side__link--active' : 'side__link'}
            aria-current={screen === 'fleet' ? 'page' : undefined}
            onClick={() => {
              setScreen('fleet')
              setOpenServer(null)
            }}
          >
            <FleetIcon />
            {t('nav.fleet')}
          </button>
          <button
            type="button"
            className={screen === 'settings' ? 'side__link side__link--active' : 'side__link'}
            aria-current={screen === 'settings' ? 'page' : undefined}
            onClick={() => {
              setScreen('settings')
              setOpenServer(null)
            }}
          >
            <GearIcon />
            {t('nav.settings')}
          </button>
          {canAudit ? (
            <button
              type="button"
              className={screen === 'audit' ? 'side__link side__link--active' : 'side__link'}
              aria-current={screen === 'audit' ? 'page' : undefined}
              onClick={() => {
                setScreen('audit')
                setOpenServer(null)
              }}
            >
              <AuditIcon />
              {t('nav.audit')}
            </button>
          ) : null}
          <button
            type="button"
            className={screen === 'about' ? 'side__link side__link--active' : 'side__link'}
            aria-current={screen === 'about' ? 'page' : undefined}
            onClick={() => setScreen('about')}
          >
            <InfoIcon />
            {t('nav.about')}
          </button>
        </nav>

        <div className="side__foot">
          <div className="side__prefs">
            <LanguageSelector />
            <ThemeToggle />
          </div>
          {auth !== null && (
            <div className="side__user">
              <span className="side__avatar" aria-hidden="true">
                {initials(auth.username)}
              </span>
              <span className="side__username">{auth.username}</span>
              <button
                type="button"
                className="side__signout"
                onClick={() => {
                  void auth.signOut()
                }}
              >
                <SignOutIcon />
                <span className="side__signout-text">{t('auth:signOut')}</span>
              </button>
            </div>
          )}
        </div>
      </aside>

      <main className="shell__main">
        <div className="shell__page" key={`${screen}-${openServer?.id ?? ''}`}>
          {screen === 'about' ? (
            <About />
          ) : screen === 'settings' ? (
            <Settings api={systemApi} />
          ) : screen === 'audit' ? (
            <Audit api={auditApi} />
          ) : openServer !== null ? (
            <ServerScreen
              server={openServer}
              api={containersApi}
              aptApi={aptApi}
              servicesApi={servicesApi}
              journalApi={journalApi}
              onBack={() => setOpenServer(null)}
            />
          ) : (
            <Fleet client={client} onOpenServer={setOpenServer} />
          )}
        </div>
      </main>
    </div>
  )
}
