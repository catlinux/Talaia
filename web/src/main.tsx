import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import { AuthGate } from './auth/AuthGate'
import { applyTheme, getInitialTheme } from './theme/theme'
import './fonts'
import './i18n'
import './styles/tokens.css'
import './styles/global.css'
import './styles/fleet.css'
import './styles/server.css'
import './styles/auth.css'

// Apply the theme before the first render to avoid a flash of the wrong theme.
applyTheme(getInitialTheme())

const container = document.getElementById('root')

if (!container) {
  throw new Error('Root element #root not found')
}

createRoot(container).render(
  <StrictMode>
    <AuthGate>
      <App />
    </AuthGate>
  </StrictMode>,
)
