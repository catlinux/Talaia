import { useTranslation } from 'react-i18next'

import { About } from './screens/About'
import { LanguageSelector } from './components/LanguageSelector'
import { ThemeToggle } from './theme/ThemeToggle'

export function App() {
  const { t } = useTranslation('common')

  return (
    <div className="app">
      <header className="app__bar">
        <span className="app__name">{t('appName')}</span>
        <div className="app__controls">
          <LanguageSelector />
          <ThemeToggle />
        </div>
      </header>
      <main className="app__main">
        <About />
      </main>
    </div>
  )
}
