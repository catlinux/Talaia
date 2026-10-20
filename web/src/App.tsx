import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { FleetClient } from './api/client'
import type { ContainersApi } from './api/containers'
import { About } from './screens/About'
import { Fleet } from './screens/Fleet'
import { ServerScreen } from './screens/ServerScreen'
import { useAuth } from './auth/AuthGate'
import { FleetIcon, InfoIcon, SignOutIcon } from './components/icons'
import { LanguageSelector } from './components/LanguageSelector'
import { Logo } from './components/Logo'
import { ThemeToggle } from './theme/ThemeToggle'

type Screen = 'fleet' | 'about'

interface AppProps {
  /** Data source of the fleet screen. Tests inject their own client. */
  client?: FleetClient
  /** Container API of the server screen. Tests inject their own. */
  containersApi?: ContainersApi
}

/** initials returns up to two letters to draw the avatar of the signed-in user. */
function initials(username: string): string {
  return username.trim().slice(0, 2).toUpperCase()
}

export function App({ client, containersApi }: AppProps) {
  const { t } = useTranslation(['common', 'auth'])
  const auth = useAuth()
  const [screen, setScreen] = useState<Screen>('fleet')
  // The server whose containers are open, if any; it replaces the fleet screen.
  const [openServer, setOpenServer] = useState<{ id: string; name: string } | null>(null)

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
          ) : openServer !== null ? (
            <ServerScreen
              server={openServer}
              api={containersApi}
              onBack={() => setOpenServer(null)}
            />
          ) : (
            <Fleet
              client={client}
              onOpenServer={(server) => setOpenServer({ id: server.id, name: server.name })}
            />
          )}
        </div>
      </main>
    </div>
  )
}
