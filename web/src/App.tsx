import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { About } from './screens/About'
import { Fleet } from './screens/Fleet'
import { useAuth } from './auth/AuthGate'
import { LanguageSelector } from './components/LanguageSelector'
import { ThemeToggle } from './theme/ThemeToggle'

type Screen = 'fleet' | 'about'

export function App() {
  const { t } = useTranslation(['common', 'auth'])
  const auth = useAuth()
  const [screen, setScreen] = useState<Screen>('fleet')

  return (
    <div className="app">
      <header className="app__bar">
        <span className="app__name">{t('appName')}</span>
        <nav className="app__nav" aria-label={t('nav.label')}>
          <button
            type="button"
            className={screen === 'fleet' ? 'app__tab app__tab--active' : 'app__tab'}
            aria-current={screen === 'fleet' ? 'page' : undefined}
            onClick={() => setScreen('fleet')}
          >
            {t('nav.fleet')}
          </button>
          <button
            type="button"
            className={screen === 'about' ? 'app__tab app__tab--active' : 'app__tab'}
            aria-current={screen === 'about' ? 'page' : undefined}
            onClick={() => setScreen('about')}
          >
            {t('nav.about')}
          </button>
        </nav>
        <div className="app__controls">
          <LanguageSelector />
          <ThemeToggle />
          {auth !== null && (
            <button
              type="button"
              className="app__signout"
              onClick={() => {
                void auth.signOut()
              }}
            >
              {t('auth:signOut')}
            </button>
          )}
        </div>
      </header>
      <main className="app__main">{screen === 'fleet' ? <Fleet /> : <About />}</main>
    </div>
  )
}
