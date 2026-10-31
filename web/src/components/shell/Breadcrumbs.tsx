import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'

import { Link, useRoute } from '../../router'

interface BreadcrumbsProps {
  serverName?: string
}

interface Crumb {
  label: string
  to?: string
}

/** Breadcrumbs derives the trail of the current route. */
export function Breadcrumbs({ serverName }: BreadcrumbsProps) {
  const { t } = useTranslation('common')
  const { path } = useRoute()
  const routePath = path.length > 1 ? path.replace(/\/+$/, '') : path

  let crumbs: Crumb[]
  if (routePath === '/') {
    crumbs = [{ label: t('nav.overview') }]
  } else if (routePath === '/servers' || routePath.startsWith('/servers/')) {
    crumbs = [{ label: t('nav.group.infrastructure') }, { label: t('nav.servers'), to: '/servers' }]
    const serverID = routePath.split('/')[2] ?? ''
    if (serverID !== '') {
      crumbs.push({ label: serverName ?? serverID })
    }
  } else if (routePath === '/containers') {
    crumbs = [{ label: t('nav.group.infrastructure') }, { label: t('nav.containers') }]
  } else if (routePath === '/stacks') {
    crumbs = [{ label: t('nav.group.infrastructure') }, { label: t('nav.stacks') }]
  } else if (routePath === '/alerts' || routePath.startsWith('/alerts/')) {
    crumbs = [{ label: t('nav.group.monitoring') }, { label: t('nav.alerts') }]
  } else if (routePath === '/audit' || routePath.startsWith('/audit/')) {
    crumbs = [{ label: t('nav.group.monitoring') }, { label: t('nav.audit') }]
  } else if (routePath === '/users' || routePath.startsWith('/users/')) {
    crumbs = [{ label: t('nav.group.administration') }, { label: t('nav.users') }]
  } else if (routePath === '/catalog') {
    crumbs = [{ label: t('nav.group.administration') }, { label: t('nav.catalog') }]
  } else if (routePath === '/settings' || routePath.startsWith('/settings/')) {
    crumbs = [{ label: t('nav.group.administration') }, { label: t('nav.settings') }]
  } else if (routePath === '/about') {
    crumbs = [{ label: t('nav.about') }]
  } else {
    crumbs = [{ label: t('shell.notFound') }]
  }

  return (
    <nav className="shell__crumbs" aria-label={t('shell.breadcrumbs')}>
      {crumbs.map((crumb, index) => {
        const last = index === crumbs.length - 1
        return (
          <Fragment key={`${crumb.label}-${index}`}>
            {index > 0 ? (
              <span className="shell__crumb-separator" aria-hidden="true">
                ›
              </span>
            ) : null}
            {crumb.to !== undefined && !last ? (
              <Link to={crumb.to}>{crumb.label}</Link>
            ) : (
              <span aria-current={last ? 'page' : undefined}>{crumb.label}</span>
            )}
          </Fragment>
        )
      })}
    </nav>
  )
}
