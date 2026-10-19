import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import { applyTheme, getInitialTheme } from './theme/theme'
import './i18n'
import './styles/tokens.css'
import './styles/global.css'
import './styles/fleet.css'

// Apply the theme before the first render to avoid a flash of the wrong theme.
applyTheme(getInitialTheme())

const container = document.getElementById('root')

if (!container) {
  throw new Error('Root element #root not found')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
