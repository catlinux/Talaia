import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Link, matchRoute, RouterProvider, useNavigate, useRoute, useSetQuery } from './router'

function RouteProbe() {
  const { path, params, query } = useRoute('/servers/:id')
  const navigate = useNavigate()
  const setQuery = useSetQuery()
  return (
    <div>
      <span data-testid="path">{path}</span>
      <span data-testid="param">{params.id ?? ''}</span>
      <span data-testid="query">{query.toString()}</span>
      <button type="button" onClick={() => navigate('/settings')}>
        go to settings
      </button>
      <button type="button" onClick={() => setQuery({ tab: 'history', page: null })}>
        set query
      </button>
    </div>
  )
}

beforeEach(() => {
  window.history.replaceState({}, '', '/')
  document.documentElement.scrollTop = 0
  document.body.scrollTop = 0
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('matchRoute', () => {
  it('matches exact paths and normalises trailing slashes', () => {
    expect(matchRoute('/', '/')).toEqual({ params: {} })
    expect(matchRoute('/servers', '/servers/')).toEqual({ params: {} })
    expect(matchRoute('/servers', '/users')).toBeNull()
    expect(matchRoute('/servers/:id', '/servers')).toBeNull()
    expect(matchRoute('/servers/:id', '/servers/abc/more')).toBeNull()
  })

  it('extracts and decodes named parameters', () => {
    expect(matchRoute('/servers/:id', '/servers/abc')).toEqual({ params: { id: 'abc' } })
    expect(matchRoute('/servers/:id/:tab', '/servers/a%20b/logs')).toEqual({
      params: { id: 'a b', tab: 'logs' },
    })
  })

  it('matches a trailing wildcard', () => {
    expect(matchRoute('/servers/:id/*', '/servers/abc/containers')).toEqual({
      params: { id: 'abc' },
    })
    expect(matchRoute('/servers/:id/*', '/servers/abc/containers/logs')).toEqual({
      params: { id: 'abc' },
    })
    expect(matchRoute('/servers/:id/*', '/servers/abc')).toEqual({ params: { id: 'abc' } })
    expect(matchRoute('/servers/*/logs', '/servers/abc/logs')).toBeNull()
  })
})

describe('RouterProvider', () => {
  it('exposes the path, query and matched params', () => {
    window.history.replaceState({}, '', '/servers/abc?tab=logs&severity=critical')
    render(
      <RouterProvider>
        <RouteProbe />
      </RouterProvider>,
    )

    expect(screen.getByTestId('path').textContent).toBe('/servers/abc')
    expect(screen.getByTestId('param').textContent).toBe('abc')
    expect(screen.getByTestId('query').textContent).toBe('tab=logs&severity=critical')
  })

  it('rewrites the query with replace and no scroll change', () => {
    window.history.replaceState({}, '', '/servers/abc?tab=logs')
    const replace = vi.spyOn(window.history, 'replaceState')
    render(
      <RouterProvider>
        <RouteProbe />
      </RouterProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'set query' }))

    expect(window.location.pathname).toBe('/servers/abc')
    expect(window.location.search).toBe('?tab=history')
    expect(screen.getByTestId('query').textContent).toBe('tab=history')
    expect(replace).toHaveBeenCalled()
  })

  it('navigates with a plain click, updates the route and scrolls to the top', () => {
    document.documentElement.scrollTop = 120
    window.history.replaceState({}, '', '/')
    render(
      <RouterProvider>
        <Link to="/settings">Settings</Link>
        <RouteProbe />
      </RouterProvider>,
    )

    fireEvent.click(screen.getByRole('link', { name: 'Settings' }))

    expect(window.location.pathname).toBe('/settings')
    expect(screen.getByTestId('path').textContent).toBe('/settings')
    expect(document.documentElement.scrollTop).toBe(0)
  })

  it('does not intercept ctrl-click', () => {
    window.history.replaceState({}, '', '/')
    const push = vi.spyOn(window.history, 'pushState')
    render(
      <RouterProvider>
        <Link to="/settings">Settings</Link>
      </RouterProvider>,
    )

    const preventCtrlNavigation = (event: MouseEvent) => {
      if (event.ctrlKey) event.preventDefault()
    }
    document.addEventListener('click', preventCtrlNavigation)
    fireEvent.click(screen.getByRole('link', { name: 'Settings' }), { ctrlKey: true })
    document.removeEventListener('click', preventCtrlNavigation)

    expect(push).not.toHaveBeenCalled()
    expect(window.location.pathname).toBe('/')
  })

  it('updates the route on popstate', async () => {
    window.history.replaceState({}, '', '/servers/one')
    render(
      <RouterProvider>
        <RouteProbe />
      </RouterProvider>,
    )

    window.history.replaceState({}, '', '/servers/two')
    window.dispatchEvent(new PopStateEvent('popstate'))

    await waitFor(() => expect(screen.getByTestId('path').textContent).toBe('/servers/two'))
    expect(screen.getByTestId('param').textContent).toBe('two')
  })
})
