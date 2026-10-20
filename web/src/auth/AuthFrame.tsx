import type { ReactNode } from 'react'

import { Logo } from '../components/Logo'

/** AuthFrame is the stage of the sign-in screens: the brand above the card. */
export function AuthFrame({ children }: { children: ReactNode }) {
  return (
    <div className="auth">
      <div className="auth__brand">
        <Logo className="auth__logo" />
        <span className="auth__wordmark">Talaia</span>
      </div>
      {children}
    </div>
  )
}
