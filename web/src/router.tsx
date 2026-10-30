import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ComponentPropsWithoutRef,
  type MouseEvent,
  type ReactNode,
} from 'react'

export type RouteParams = Record<string, string>
export type QueryUpdates = Record<string, string | number | boolean | null | undefined>

export interface RouteMatch {
  params: RouteParams
}

interface RouterContextValue {
  path: string
  params: RouteParams
  query: URLSearchParams
  navigate: typeof navigate
  setQuery: typeof setQuery
}

const RouterContext = createContext<RouterContextValue | null>(null)

const ROUTE_CHANGE_EVENT = 'talaia:route-change'

function cleanPath(path: string): string {
  const withoutQuery = path.split(/[?#]/, 1)[0] ?? '/'
  const withSlash = withoutQuery.startsWith('/') ? withoutQuery : `/${withoutQuery}`
  if (withSlash.length > 1 && withSlash.endsWith('/')) {
    return withSlash.replace(/\/+$/, '') || '/'
  }
  return withSlash || '/'
}

function pathSegments(path: string): string[] {
  const clean = cleanPath(path)
  return clean === '/' ? [] : clean.slice(1).split('/')
}

/** matchRoute matches a path against a pattern with :params and a trailing *. */
export function matchRoute(pattern: string, path: string): RouteMatch | null {
  const patternParts = pathSegments(pattern)
  const pathParts = pathSegments(path)
  const wildcard = patternParts[patternParts.length - 1] === '*'
  if (patternParts.some((part, index) => part === '*' && index !== patternParts.length - 1)) {
    return null
  }
  if (wildcard) {
    patternParts.pop()
    if (pathParts.length < patternParts.length) return null
  } else if (pathParts.length !== patternParts.length) {
    return null
  }

  const params: RouteParams = {}
  for (let index = 0; index < patternParts.length; index += 1) {
    const expected = patternParts[index]!
    const actual = pathParts[index]!
    if (expected.startsWith(':')) {
      if (actual === '') return null
      try {
        params[expected.slice(1)] = decodeURIComponent(actual)
      } catch {
        params[expected.slice(1)] = actual
      }
    } else if (expected !== actual) {
      return null
    }
  }
  return { params }
}

function readLocation(): { path: string; query: URLSearchParams } {
  return {
    path: window.location.pathname,
    query: new URLSearchParams(window.location.search),
  }
}

/** navigate changes the current URL and tells the router to re-render. */
export function navigate(to: string, options?: { replace?: boolean }): void {
  const url = new URL(to, window.location.href)
  const target = `${url.pathname}${url.search}${url.hash}`
  if (options?.replace === true) window.history.replaceState({}, '', target)
  else window.history.pushState({}, '', target)
  window.dispatchEvent(new Event(ROUTE_CHANGE_EVENT))
  if (options?.replace !== true) {
    document.documentElement.scrollTop = 0
    document.body.scrollTop = 0
  }
}

/** setQuery rewrites the query string without adding a history entry. */
export function setQuery(updates: QueryUpdates): void {
  const url = new URL(window.location.href)
  for (const [key, value] of Object.entries(updates)) {
    if (value === null || value === undefined || value === '') url.searchParams.delete(key)
    else url.searchParams.set(key, String(value))
  }
  window.history.replaceState({}, '', `${url.pathname}${url.search}${url.hash}`)
  window.dispatchEvent(new Event(ROUTE_CHANGE_EVENT))
}

/** RouterProvider keeps the route in React state and listens to Back. */
export function RouterProvider({ children }: { children: ReactNode }) {
  const [location, setLocation] = useState(readLocation)

  useEffect(() => {
    const syncLocation = () => setLocation(readLocation())
    window.addEventListener('popstate', syncLocation)
    window.addEventListener(ROUTE_CHANGE_EVENT, syncLocation)
    return () => {
      window.removeEventListener('popstate', syncLocation)
      window.removeEventListener(ROUTE_CHANGE_EVENT, syncLocation)
    }
  }, [])

  const value = useMemo<RouterContextValue>(
    () => ({
      path: location.path,
      params: {},
      query: location.query,
      navigate,
      setQuery,
    }),
    [location],
  )

  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>
}

function useRouter(): RouterContextValue {
  const context = useContext(RouterContext)
  if (context === null) throw new Error('The router hooks need a RouterProvider')
  return context
}

/** useRoute returns the current path, query and any params matched by pattern. */
export function useRoute(pattern?: string): {
  path: string
  params: RouteParams
  query: URLSearchParams
} {
  const route = useRouter()
  const params =
    pattern === undefined ? route.params : (matchRoute(pattern, route.path)?.params ?? {})
  return { path: route.path, params, query: route.query }
}

/** useNavigate returns the history navigation function. */
export function useNavigate(): RouterContextValue['navigate'] {
  return useRouter().navigate
}

/** useSetQuery returns the query-string replacement helper. */
export function useSetQuery(): RouterContextValue['setQuery'] {
  return useRouter().setQuery
}

type LinkProps = Omit<ComponentPropsWithoutRef<'a'>, 'href'> & {
  to: string
  replace?: boolean
}

/** Link renders an internal link and navigates with the History API. */
export function Link({ to, replace, onClick, target, ...props }: LinkProps) {
  const navigate = useNavigate()
  const handleClick = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event)
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey ||
      (target !== undefined && target !== '_self')
    ) {
      return
    }
    event.preventDefault()
    navigate(to, { replace })
  }
  return <a {...props} href={to} target={target} onClick={handleClick} />
}
