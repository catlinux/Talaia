import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthGate'
import { supportedLanguages, type SupportedLanguage } from '../../i18n'
import { CheckIcon, InfoIcon, SignOutIcon } from '../icons'
import { Menu, type MenuEntry } from '../ui'
import { useNavigate } from '../../router'
import { applyTheme, getInitialTheme, storeTheme, type ThemeMode } from '../../theme/theme'

const themeOptions: ThemeMode[] = ['system', 'light', 'dark']

function currentLanguage(resolved: string | undefined, fallback: string): SupportedLanguage {
  const language = resolved ?? fallback
  return supportedLanguages.find((candidate) => language.startsWith(candidate)) ?? 'en'
}

function initials(username: string): string {
  return username.trim().slice(0, 2).toUpperCase()
}

/** UserMenu is the account menu of the top bar. */
export function UserMenu() {
  const { t, i18n } = useTranslation(['common', 'auth'])
  const auth = useAuth()
  const navigate = useNavigate()
  const username = auth?.username ?? ''
  const language = currentLanguage(i18n.resolvedLanguage, i18n.language)
  const [theme, setTheme] = useState<ThemeMode>(getInitialTheme)

  const check = (selected: boolean) =>
    selected ? <CheckIcon /> : <span className="shell__menu-check" aria-hidden="true" />

  const items: MenuEntry[] = [
    { heading: username },
    { separator: true },
    { heading: t('language.label') },
    ...supportedLanguages.map((candidate) => ({
      id: `language-${candidate}`,
      label: t(`language.${candidate}`),
      icon: check(candidate === language),
      onSelect: () => {
        void i18n.changeLanguage(candidate)
      },
    })),
    { separator: true },
    { heading: t('theme.label') },
    ...themeOptions.map((candidate) => ({
      id: `theme-${candidate}`,
      label: t(`theme.${candidate}`),
      icon: check(candidate === theme),
      onSelect: () => {
        applyTheme(candidate)
        storeTheme(candidate)
        setTheme(candidate)
      },
    })),
    { separator: true },
    {
      id: 'about',
      label: t('nav.about'),
      icon: <InfoIcon />,
      onSelect: () => navigate('/about'),
    },
    {
      id: 'signout',
      label: t('auth:signOut'),
      icon: <SignOutIcon />,
      danger: true,
      onSelect: () => {
        void auth?.signOut()
      },
    },
  ]

  return (
    <Menu
      align="end"
      items={items}
      trigger={(props) => (
        <button
          {...props}
          className="shell__user-button"
          aria-label={username === '' ? t('shell.userMenu') : username}
          title={username}
        >
          <span className="shell__avatar" aria-hidden="true">
            {initials(username)}
          </span>
        </button>
      )}
    />
  )
}
