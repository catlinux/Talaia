import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { App } from './App'
import i18n from './i18n'
import { ATTRIBUTION } from './screens/About'

const titles = {
  ca: 'Sobre Talaia',
  es: 'Acerca de Talaia',
  en: 'About Talaia',
} as const

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

  it('renders the About screen in English by default', () => {
    render(<App />)

    expect(i18n.resolvedLanguage).toBe('en')
    expect(currentTitle()).toBe(titles.en)
    expect(screen.getByText(__APP_VERSION__)).toBeTruthy()
  })

  it('links the license to the repository', () => {
    render(<App />)

    const licenseLink = screen.getByRole('link', { name: 'WNCL-1.0' })

    expect(licenseLink.getAttribute('href')).toBe('https://github.com/catlinux/Talaia')
  })

  it('changes the title when the language switches to Catalan', async () => {
    render(<App />)

    await i18n.changeLanguage('ca')

    expect(currentTitle()).toBe(titles.ca)
  })

  it('shows the attribution text in all three languages', async () => {
    render(<App />)

    for (const language of ['ca', 'es', 'en'] as const) {
      await i18n.changeLanguage(language)
      expect(currentTitle()).toBe(titles[language])
      expect(screen.getByText(ATTRIBUTION)).toBeTruthy()
    }
  })
})
