import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import type { AlertSummary } from '../../api/alerts'
import { usePermissions } from '../../auth/permissions'
import { Logo } from '../Logo'
import { AlertIcon, AuditIcon, CloseIcon, FleetIcon, GearIcon, LogsIcon, UsersIcon } from '../icons'
import { AlertsBadge } from '../../screens/Alerts'
import { Link, useRoute } from '../../router'

const SIDEBAR_STORAGE_KEY = 'talaia.sidebar'

function readCollapsed(): boolean {
  try {
    const value = localStorage.getItem(SIDEBAR_STORAGE_KEY)
    return value === '1' || value === 'true' || value === 'collapsed'
  } catch {
    return false
  }
}

function storeCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(SIDEBAR_STORAGE_KEY, collapsed ? '1' : '0')
  } catch {
    // The sidebar still works for this session when storage is unavailable.
  }
}

interface SidebarProps {
  alertSummary: AlertSummary | null
  mobileOpen: boolean
  onClose: () => void
}

interface NavItem {
  to: string
  label: string
  icon: ReactNode
  active: boolean
  badge?: ReactNode
}

/** Sidebar is the grouped, collapsible navigation of the panel. */
export function Sidebar({ alertSummary, mobileOpen, onClose }: SidebarProps) {
  const { t } = useTranslation('common')
  const { path } = useRoute()
  const { can, canAnywhere } = usePermissions()
  const [collapsed, setCollapsed] = useState(readCollapsed)

  const canUsers = can('users.manage')
  const canAudit = can('audit.view') || can('recordings.view')
  const canAlerts = canAnywhere('alerts.view')
  const canContainers = canAnywhere('container.view')
  const canStacks = canAnywhere('stack.view')

  useEffect(() => {
    if (!mobileOpen) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [mobileOpen, onClose])

  const isActive = (to: string) =>
    to === '/' ? path === '/' : path === to || path.startsWith(`${to}/`)
  const linkClass = (active: boolean) => (active ? 'side__link side__link--active' : 'side__link')

  const infrastructure: NavItem[] = [
    { to: '/servers', label: t('nav.servers'), icon: <FleetIcon />, active: isActive('/servers') },
  ]
  if (canContainers)
    infrastructure.push({
      to: '/containers',
      label: t('nav.containers'),
      icon: <LogsIcon />,
      active: isActive('/containers'),
    })
  if (canStacks)
    infrastructure.push({
      to: '/stacks',
      label: t('nav.stacks'),
      icon: <FleetIcon />,
      active: isActive('/stacks'),
    })

  const monitoring: NavItem[] = []
  if (canAlerts)
    monitoring.push({
      to: '/alerts',
      label: t('nav.alerts'),
      icon: <AlertIcon />,
      active: isActive('/alerts'),
      badge: <AlertsBadge summary={alertSummary} />,
    })
  if (canAudit)
    monitoring.push({
      to: '/audit',
      label: t('nav.audit'),
      icon: <AuditIcon />,
      active: isActive('/audit'),
    })

  const administration: NavItem[] = []
  if (canUsers)
    administration.push({
      to: '/users',
      label: t('nav.users'),
      icon: <UsersIcon />,
      active: isActive('/users'),
    })
  administration.push({
    to: '/settings',
    label: t('nav.settings'),
    icon: <GearIcon />,
    active: isActive('/settings'),
  })

  const toggle = () => {
    const next = !collapsed
    setCollapsed(next)
    storeCollapsed(next)
  }

  const closeAfterNavigate = () => {
    if (mobileOpen) onClose()
  }

  return (
    <>
      {mobileOpen ? (
        <div className="shell__nav-overlay" role="presentation" onClick={onClose} />
      ) : null}
      <aside
        className={[
          'side',
          'shell__sidebar',
          collapsed ? 'side--collapsed' : '',
          mobileOpen ? 'side--open' : '',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        <div className="side__brand shell__side-brand">
          <Link
            to="/"
            className="shell__brand-link"
            aria-label={t('appName')}
            onClick={closeAfterNavigate}
          >
            <Logo className="side__logo" />
            <span className="side__name">{t('appName')}</span>
          </Link>
          <button
            type="button"
            className="shell__nav-close"
            aria-label={t('shell.closeMenu')}
            onClick={onClose}
          >
            <CloseIcon />
          </button>
        </div>

        <nav className="side__nav" aria-label={t('nav.label')}>
          <Link
            to="/"
            className={linkClass(path === '/')}
            aria-current={path === '/' ? 'page' : undefined}
            onClick={closeAfterNavigate}
          >
            <FleetIcon />
            <span className="shell__nav-label">{t('nav.overview')}</span>
          </Link>

          <div className="side__group">{t('nav.group.infrastructure')}</div>
          {infrastructure.map((item) => (
            <NavLink
              key={item.to}
              item={item}
              className={linkClass(item.active)}
              onNavigate={closeAfterNavigate}
            />
          ))}

          {monitoring.length > 0 ? (
            <>
              <div className="side__group">{t('nav.group.monitoring')}</div>
              {monitoring.map((item) => (
                <NavLink
                  key={item.to}
                  item={item}
                  className={linkClass(item.active)}
                  onNavigate={closeAfterNavigate}
                />
              ))}
            </>
          ) : null}

          <div className="side__group">{t('nav.group.administration')}</div>
          {administration.map((item) => (
            <NavLink
              key={item.to}
              item={item}
              className={linkClass(item.active)}
              onNavigate={closeAfterNavigate}
            />
          ))}
        </nav>

        <div className="shell__side-foot">
          <span className="shell__version">{t('shell.version', { version: __APP_VERSION__ })}</span>
          <button
            type="button"
            className="shell__collapse"
            aria-label={t(collapsed ? 'shell.expand' : 'shell.collapse')}
            aria-expanded={!collapsed}
            onClick={toggle}
          >
            <span aria-hidden="true">{collapsed ? '»' : '«'}</span>
          </button>
        </div>
      </aside>
    </>
  )
}

function NavLink({
  item,
  className,
  onNavigate,
}: {
  item: NavItem
  className: string
  onNavigate: () => void
}) {
  return (
    <Link
      to={item.to}
      className={className}
      aria-current={item.active ? 'page' : undefined}
      onClick={onNavigate}
    >
      {item.icon}
      <span className="shell__nav-label">{item.label}</span>
      {item.badge}
    </Link>
  )
}
