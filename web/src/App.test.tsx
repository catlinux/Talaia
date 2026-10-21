import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { App } from './App'
import { AuthGate } from './auth/AuthGate'
import type { AuthApi } from './api/auth'
import { createMockFleetClient } from './api/client'
import type { ContainersApi } from './api/containers'
import type { SystemApi } from './api/system'
import i18n from './i18n'
import { ATTRIBUTION } from './screens/About'

const fleetTitles = { ca: 'Flota', es: 'Flota', en: 'Fleet' } as const
const settingsTitles = { ca: 'Configuració', es: 'Ajustes', en: 'Settings' } as const
const aboutTitles = { ca: 'Sobre Talaia', es: 'Acerca de Talaia', en: 'About Talaia' } as const
const navFleet = { ca: 'Flota', es: 'Flota', en: 'Fleet' } as const
const navSettings = { ca: 'Configuració', es: 'Ajustes', en: 'Settings' } as const
const navAbout = { ca: 'Sobre', es: 'Acerca de', en: 'About' } as const

/** signedInApi is the authentication API of an already verified session. */
const signedInApi: AuthApi = {
  session: () => Promise.resolve({ username: 'admin', stage: 'full' }),
  login: () => Promise.reject(new Error('the panel is already signed in')),
  startTotpSetup: () => Promise.reject(new Error('the panel is already signed in')),
  confirmTotpSetup: () => Promise.reject(new Error('the panel is already signed in')),
  verifyTotp: () => Promise.reject(new Error('the panel is already signed in')),
  logout: () => Promise.resolve(),
}

/** renderApp mounts the panel behind an authenticated gate. */
function renderApp(systemApi?: SystemApi) {
  render(
    <AuthGate api={signedInApi}>
      <App client={createMockFleetClient()} systemApi={systemApi} />
    </AuthGate>,
  )
}

function currentTitle(): string {
  return screen.getByRole('heading', { level: 1 }).textContent ?? ''
}

describe('App', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('shows the Fleet screen by default', async () => {
    renderApp()

    expect(i18n.resolvedLanguage).toBe('en')
    expect(await screen.findByText('vps-web-01')).toBeTruthy()
    expect(currentTitle()).toBe(fleetTitles.en)
  })

  it('navigates to About and back to the Fleet screen', async () => {
    renderApp()
    await screen.findByText('vps-web-01')

    fireEvent.click(screen.getByRole('button', { name: navAbout.en }))

    expect(currentTitle()).toBe(aboutTitles.en)
    expect(screen.getByText(ATTRIBUTION)).toBeTruthy()
    expect(screen.getByRole('link', { name: 'WNCL-1.0' }).getAttribute('href')).toBe(
      'https://github.com/catlinux/Talaia',
    )

    fireEvent.click(screen.getByRole('button', { name: navFleet.en }))
    expect(currentTitle()).toBe(fleetTitles.en)
  })

  it('shows Settings between Fleet and About and renders the version', async () => {
    const systemApi: SystemApi = {
      version: () =>
        Promise.resolve({
          version: '1.2.3',
          commit: 'a'.repeat(40),
          update: {
            current: 'a'.repeat(40),
            latest: 'a'.repeat(40),
            pending: [],
            ci: 'green',
            checkedAt: '2026-10-21T08:00:00Z',
            error: '',
            updateAvailable: false,
          },
          status: {
            state: 'idle',
            from: '',
            to: '',
            startedAt: '',
            finishedAt: '',
            log: [],
          },
        }),
      check: () =>
        Promise.resolve({
          version: '1.2.3',
          commit: 'a'.repeat(40),
          update: {
            current: 'a'.repeat(40),
            latest: 'a'.repeat(40),
            pending: [],
            ci: 'green',
            checkedAt: '2026-10-21T08:00:00Z',
            error: '',
            updateAvailable: false,
          },
          status: {
            state: 'idle',
            from: '',
            to: '',
            startedAt: '',
            finishedAt: '',
            log: [],
          },
        }),
      update: () => Promise.reject(new Error('unexpected update')),
    }
    renderApp(systemApi)
    await screen.findByText('vps-web-01')

    const nav = screen.getByRole('navigation', { name: 'Main navigation' })
    expect(
      within(nav)
        .getAllByRole('button')
        .map((button) => button.textContent),
    ).toEqual([navFleet.en, navSettings.en, navAbout.en])

    fireEvent.click(screen.getByRole('button', { name: navSettings.en }))

    expect(currentTitle()).toBe(settingsTitles.en)
    expect(await screen.findByText('1.2.3')).toBeTruthy()
  })

  it('keeps the attribution text in every language', async () => {
    renderApp()
    await screen.findByText('vps-web-01')

    for (const language of ['ca', 'es', 'en'] as const) {
      await i18n.changeLanguage(language)
      fireEvent.click(screen.getByRole('button', { name: navAbout[language] }))
      expect(currentTitle()).toBe(aboutTitles[language])
      expect(screen.getByText(ATTRIBUTION)).toBeTruthy()
    }
  })

  it('translates the navigation and the Fleet title with the language', async () => {
    renderApp()
    await screen.findByText('vps-web-01')

    await i18n.changeLanguage('ca')

    expect(currentTitle()).toBe(fleetTitles.ca)
    expect(screen.getByRole('button', { name: navFleet.ca })).toBeTruthy()
    expect(screen.getByRole('button', { name: navAbout.ca })).toBeTruthy()
  })

  it('opens the containers of a server and goes back to the fleet', async () => {
    const containersApi: ContainersApi = {
      list: () =>
        Promise.resolve({
          connected: true,
          dockerAvailable: true,
          containers: [
            {
              id: 'aaaaaaaaaaaa',
              name: 'shop-web-1',
              image: 'nginx',
              state: 'running',
              status: 'Up',
              composeProject: 'shop',
              composeService: 'web',
            },
          ],
        }),
      run: () => Promise.reject(new Error('unexpected')),
      logs: () => Promise.reject(new Error('unexpected')),
      stacks: () => Promise.resolve({ connected: true, stacks: [], adopted: [], adoptable: [] }),
      adoptStack: () => Promise.reject(new Error('unexpected')),
      listResources: () => Promise.reject(new Error('unexpected')),
      removeResources: () => Promise.reject(new Error('unexpected')),
      writeStack: () => Promise.reject(new Error('unexpected')),
      readStack: () => Promise.reject(new Error('unexpected')),
      runStack: () => Promise.reject(new Error('unexpected')),
      job: () => Promise.reject(new Error('unexpected')),
    }
    render(
      <AuthGate api={signedInApi}>
        <App client={createMockFleetClient()} containersApi={containersApi} />
      </AuthGate>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'vps-web-01' }))

    expect(await screen.findByText('shop-web-1')).toBeTruthy()
    expect(currentTitle()).toBe('vps-web-01')

    fireEvent.click(screen.getByRole('button', { name: 'Back to the fleet' }))
    expect(currentTitle()).toBe(fleetTitles.en)
  })
})
