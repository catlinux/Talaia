import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import type { FleetClient } from '../api/client'
import i18n from '../i18n'
import { Fleet } from './Fleet'

const serverNames = ['vps-web-01', 'vps-db-01', 'vps-mail', 'vps-dev', 'vps-backup', 'rpi-casa']

describe('Fleet', () => {
  beforeEach(async () => {
    localStorage.clear()
    await i18n.changeLanguage('en')
  })

  afterEach(() => {
    cleanup()
  })

  it('renders the KPI row and the six servers of the mock client', async () => {
    render(<Fleet />)

    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Fleet')

    expect(await screen.findByText('vps-web-01')).toBeTruthy()

    expect(screen.getByText('Servers online')).toBeTruthy()
    expect(screen.getByText('Running containers')).toBeTruthy()
    expect(screen.getByText('Security updates')).toBeTruthy()
    expect(screen.getByText('Active alerts')).toBeTruthy()
    expect(screen.getByText('Last backup')).toBeTruthy()
    expect(screen.getByText('5 / 6')).toBeTruthy()

    for (const name of serverNames) {
      expect(screen.getByText(name)).toBeTruthy()
    }

    // Three meters (CPU, RAM, disk) per server.
    expect(screen.getAllByRole('meter')).toHaveLength(serverNames.length * 3)
  })

  it('shows the text status of a critical server', async () => {
    render(<Fleet />)

    expect(await screen.findByText('vps-db-01')).toBeTruthy()
    expect(screen.getByText('Disk at 91%')).toBeTruthy()
  })

  it('shows the activity feed with translated messages', async () => {
    render(<Fleet />)

    expect(await screen.findByText(/Disk above 90% on vps-db-01/)).toBeTruthy()
    expect(screen.getByRole('heading', { name: /Activity/ })).toBeTruthy()
  })

  it('shows the error state when the client rejects', async () => {
    const client: FleetClient = {
      listServers: () => Promise.reject(new Error('no connection')),
      listActivity: () => Promise.resolve([]),
    }

    render(<Fleet client={client} />)

    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByText('The fleet could not be loaded')).toBeTruthy()
  })
})
