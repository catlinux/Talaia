import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react'
import { useTranslation } from 'react-i18next'

import { createApiFleetClient, type FleetClient } from '../../api/client'
import { createContainersApi, type ContainersApi } from '../../api/containers'
import type { Server } from '../../api/types'
import { useAuth } from '../../auth/AuthGate'
import { usePermissions } from '../../auth/permissions'
import i18n, { supportedLanguages } from '../../i18n'
import { useNavigate } from '../../router'
import { applyTheme, getInitialTheme, storeTheme } from '../../theme/theme'
import { stackPath } from '../../screens/server/serverTabs'
import { loadFleet, type FleetLoad } from '../../screens/fleetwide/loadFleet'
import {
  readPaletteRecent,
  rememberPalettePick,
  searchPalette,
  type PaletteItem,
  type PaletteSection,
} from './paletteSearch'

const cacheMs = 60_000
const maxPerSection = 8
const maxResults = 30
const sectionOrder: PaletteSection[] = [
  'screens',
  'recent',
  'servers',
  'containers',
  'stacks',
  'actions',
]

const defaultClient = createApiFleetClient()
const defaultContainersApi = createContainersApi()

interface CommandPaletteProps {
  open: boolean
  onOpen: () => void
  onClose: () => void
  client?: FleetClient
  containersApi?: ContainersApi
}

interface CachedFleet {
  servers: Server[]
  fleet: FleetLoad
  at: number
}

function isMac(): boolean {
  const navigatorWithData = navigator as Navigator & { userAgentData?: { platform?: string } }
  const platform = navigatorWithData.userAgentData?.platform ?? navigator.platform ?? ''
  return /mac|iphone|ipad|ipod/i.test(platform)
}

function emptyFleet(servers: Server[]): FleetLoad {
  return {
    servers,
    containers: [],
    stacks: [],
    adoptable: [],
    failed: [],
    notConnected: [],
  }
}

function visibleResults(items: PaletteItem[]): PaletteItem[] {
  const limited: PaletteItem[] = []
  for (const section of sectionOrder) {
    let count = 0
    for (const item of items) {
      if (item.section !== section) continue
      if (count >= maxPerSection || limited.length >= maxResults) break
      limited.push(item)
      count += 1
    }
    if (limited.length >= maxResults) break
  }
  return limited
}

/** CommandPalette searches screens, fleet data and frequent actions. */
export function CommandPalette({
  open,
  onOpen,
  onClose,
  client = defaultClient,
  containersApi = defaultContainersApi,
}: CommandPaletteProps) {
  const { t } = useTranslation('common')
  const {
    loading: permissionsLoading,
    can,
    canAnywhere,
    canSeeContainers,
    canSeeStacks,
  } = usePermissions()
  const auth = useAuth()
  const navigate = useNavigate()
  const input = useRef<HTMLInputElement>(null)
  const opener = useRef<HTMLElement | null>(null)
  const cache = useRef<CachedFleet | null>(null)
  const [query, setQuery] = useState('')
  const [activeId, setActiveId] = useState<string | null>(null)
  const [servers, setServers] = useState<Server[] | null>(null)
  const [fleet, setFleet] = useState<FleetLoad | null>(null)

  const close = useCallback(() => {
    setQuery('')
    setActiveId(null)
    onClose()
  }, [onClose])

  useEffect(() => {
    if (!open) return
    const active = document.activeElement
    opener.current = active instanceof HTMLElement ? active : null
    input.current?.focus()
    input.current?.select()
    return () => {
      const target = opener.current
      opener.current = null
      queueMicrotask(() => {
        if (target?.isConnected === true) target.focus()
      })
    }
  }, [open])

  useEffect(() => {
    if (!open || permissionsLoading) return
    const cached = cache.current
    if (cached !== null && Date.now() - cached.at < cacheMs) return

    let active = true
    client.listServers().then(
      async (list) => {
        if (!active) return
        setServers(list)
        const loaded = await loadFleet(
          { ...client, listServers: () => Promise.resolve(list) },
          containersApi,
          {
            canLoad: (server) => canSeeContainers(server.id) || canSeeStacks(server.id),
            canLoadStacks: (server) => canSeeStacks(server.id),
          },
        )
        if (!active) return
        setFleet(loaded)
        cache.current = { servers: list, fleet: loaded, at: Date.now() }
      },
      () => {
        if (!active) return
        setServers([])
        setFleet(emptyFleet([]))
      },
    )
    return () => {
      active = false
    }
  }, [canSeeContainers, canSeeStacks, client, containersApi, open, permissionsLoading])

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key.toLowerCase() !== 'k' || (!event.ctrlKey && !event.metaKey)) return
      event.preventDefault()
      if (open) {
        close()
        return
      }
      if (document.querySelector('[role="dialog"], [role="alertdialog"]') !== null) return
      onOpen()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [close, onOpen, open])

  const entries = useMemo<PaletteItem[]>(() => {
    const result: PaletteItem[] = [
      { id: 'screen-overview', section: 'screens', label: t('nav.overview'), to: '/' },
      { id: 'screen-servers', section: 'screens', label: t('nav.servers'), to: '/servers' },
    ]
    if (canAnywhere('container.view')) {
      result.push({
        id: 'screen-containers',
        section: 'screens',
        label: t('nav.containers'),
        to: '/containers',
      })
    }
    if (canAnywhere('stack.view')) {
      result.push({
        id: 'screen-stacks',
        section: 'screens',
        label: t('nav.stacks'),
        to: '/stacks',
      })
    }
    if (canAnywhere('alerts.view') || canAnywhere('alerts.manage')) {
      result.push({
        id: 'screen-alerts',
        section: 'screens',
        label: t('nav.alerts'),
        to: '/alerts',
      })
    }
    if (can('audit.view') || can('recordings.view')) {
      result.push({ id: 'screen-audit', section: 'screens', label: t('nav.audit'), to: '/audit' })
    }
    if (can('users.manage')) {
      result.push({ id: 'screen-users', section: 'screens', label: t('nav.users'), to: '/users' })
    }
    result.push(
      { id: 'screen-settings', section: 'screens', label: t('nav.settings'), to: '/settings' },
      { id: 'screen-about', section: 'screens', label: t('nav.about'), to: '/about' },
    )

    for (const server of servers ?? []) {
      result.push({
        id: `server:${server.id}`,
        section: 'servers',
        label: server.name,
        detail: t(`palette.status.${server.status}`),
        to: `/servers/${encodeURIComponent(server.id)}`,
      })
    }

    for (const row of fleet?.containers ?? []) {
      if (!canSeeContainers(row.serverId)) continue
      result.push({
        id: `container:${row.serverId}:${row.container.id}`,
        section: 'containers',
        label: row.container.name,
        detail: t('palette.containerDetail', {
          server: row.serverName,
          image: row.container.image,
        }),
        to: `/servers/${encodeURIComponent(row.serverId)}/containers`,
      })
    }

    for (const row of fleet?.stacks ?? []) {
      if (!canSeeStacks(row.serverId)) continue
      result.push({
        id: `stack:${row.serverId}:${row.stack}`,
        section: 'stacks',
        label: row.stack,
        detail: t('palette.stackDetail', { server: row.serverName }),
        to: stackPath(row.serverId, row.stack),
      })
    }

    if (can('server.enroll')) {
      result.push({
        id: 'action-add-server',
        section: 'actions',
        label: t('palette.actions.addServer'),
        to: '/servers?add=1',
      })
    }
    result.push({
      id: 'action-theme',
      section: 'actions',
      label: t('palette.actions.switchTheme'),
      action: { kind: 'theme' },
    })
    for (const language of supportedLanguages) {
      result.push({
        id: `action-language-${language}`,
        section: 'actions',
        label: t(`language.${language}`),
        detail: t('palette.actions.changeLanguage'),
        action: { kind: 'language', language },
      })
    }
    result.push({
      id: 'action-signout',
      section: 'actions',
      label: t('palette.actions.signOut'),
      action: { kind: 'signout' },
    })
    return result
  }, [can, canAnywhere, canSeeContainers, canSeeStacks, fleet, servers, t])

  const recent = readPaletteRecent()
  const results = visibleResults(searchPalette(entries, query, recent))
  const activeItem = results.find((item) => item.id === activeId) ?? results[0]
  const loading = fleet === null

  const choose = (item: PaletteItem) => {
    rememberPalettePick(item.id)
    close()
    if (item.to !== undefined) {
      navigate(item.to)
      return
    }
    switch (item.action?.kind) {
      case 'theme': {
        const next = getInitialTheme() === 'dark' ? 'light' : 'dark'
        applyTheme(next)
        storeTheme(next)
        return
      }
      case 'language':
        void i18n.changeLanguage(item.action.language)
        return
      case 'signout':
        void auth?.signOut()
        return
      default:
        return
    }
  }

  const move = (delta: number) => {
    if (results.length === 0) return
    const current = results.findIndex((item) => item.id === activeItem?.id)
    const next = (current + delta + results.length) % results.length
    setActiveId(results[next]!.id)
  }

  const onInputKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        move(1)
        break
      case 'ArrowUp':
        event.preventDefault()
        move(-1)
        break
      case 'Enter':
        event.preventDefault()
        if (activeItem !== undefined) choose(activeItem)
        break
      case 'Escape':
        event.preventDefault()
        close()
        break
      case 'Tab':
        event.preventDefault()
        break
      default:
    }
  }

  if (!open) return null

  return (
    <div
      className="palette-overlay"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) close()
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) close()
      }}
    >
      <section className="palette" role="dialog" aria-modal="true" aria-label={t('palette.title')}>
        <div className="palette__search">
          <svg className="palette__icon" viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="11" cy="11" r="7" />
            <path d="m20 20-3.5-3.5" />
          </svg>
          <input
            ref={input}
            className="palette__input"
            role="combobox"
            aria-label={t('palette.searchLabel')}
            aria-expanded={true}
            aria-controls="command-palette-list"
            aria-activedescendant={
              activeItem === undefined ? undefined : `palette-option-${activeItem.id}`
            }
            aria-autocomplete="list"
            value={query}
            placeholder={t('palette.placeholder')}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => {
              setQuery(event.target.value)
              setActiveId(null)
            }}
            onKeyDown={onInputKeyDown}
          />
          <span className="palette__kbd">{isMac() ? t('palette.cmdK') : t('palette.ctrlK')}</span>
        </div>

        <div
          className="palette__list"
          id="command-palette-list"
          role="listbox"
          aria-label={t('palette.results')}
        >
          {sectionOrder.map((section) => {
            const items = results.filter((item) => item.section === section)
            if (items.length === 0) return null
            const headingID = `palette-section-${section}`
            return (
              <div
                className="palette__section"
                role="group"
                aria-labelledby={headingID}
                key={section}
              >
                <div className="palette__heading" id={headingID} role="presentation">
                  {t(`palette.sections.${section}`)}
                </div>
                {items.map((item) => {
                  const active = item.id === activeItem?.id
                  return (
                    <div
                      className={active ? 'palette__option palette__option--on' : 'palette__option'}
                      id={`palette-option-${item.id}`}
                      role="option"
                      aria-label={item.label}
                      aria-selected={active}
                      key={item.id}
                      onMouseEnter={() => setActiveId(item.id)}
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => choose(item)}
                    >
                      <span className="palette__label">{item.label}</span>
                      {item.detail !== undefined ? (
                        <span className="palette__detail">{item.detail}</span>
                      ) : null}
                    </div>
                  )
                })}
              </div>
            )
          })}
          {loading ? (
            <p className="palette__loading" role="status">
              {t('palette.loadingContainers')}
            </p>
          ) : null}
          {results.length === 0 && !loading ? (
            <p className="palette__empty">{t('palette.empty')}</p>
          ) : null}
        </div>
      </section>
    </div>
  )
}
