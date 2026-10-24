import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

import {
  createMeApi,
  type MeApi,
  type MePermissions,
  type MeUser,
  type Permission,
} from '../api/me'

/** PermissionScope narrows a permission check to one server, stack or container. */
export interface PermissionScope {
  serverId?: string
  stack?: string
  container?: string
}

/** PermissionsContextValue is what screens use to hide controls. */
export interface PermissionsContextValue {
  loading: boolean
  user: MeUser | null
  can(permission: Permission, scope?: PermissionScope): boolean
  /** canAnywhere reports whether the permission is granted on any scope. */
  canAnywhere(permission: Permission): boolean
  canAnyOnServer(serverId: string): boolean
  canSeeStacks(serverId: string): boolean
  canSeeContainers(serverId: string): boolean
}

const emptyPermissions: MePermissions = {
  organization: [],
  servers: {},
  stacks: {},
  containers: {},
}

interface PermissionState {
  loading: boolean
  user: MeUser | null
  permissions: MePermissions
}

const PermissionsContext = createContext<PermissionsContextValue | null>(null)

function hasPermissions(list: Permission[] | undefined, permission: Permission): boolean {
  return list?.includes(permission) ?? false
}

function makeCan(state: PermissionState) {
  return (permission: Permission, scope?: PermissionScope): boolean => {
    if (state.loading) return false
    const permissions = state.permissions
    if (scope?.serverId === undefined) {
      return hasPermissions(permissions.organization, permission)
    }
    const serverId = scope.serverId
    if (scope.container !== undefined) {
      if (hasPermissions(permissions.containers[serverId]?.[scope.container], permission)) {
        return true
      }
      if (
        scope.stack !== undefined &&
        hasPermissions(permissions.stacks[serverId]?.[scope.stack], permission)
      ) {
        return true
      }
    }
    if (
      scope.stack !== undefined &&
      hasPermissions(permissions.stacks[serverId]?.[scope.stack], permission)
    ) {
      return true
    }
    if (hasPermissions(permissions.servers[serverId], permission)) {
      return true
    }
    return hasPermissions(permissions.organization, permission)
  }
}

function makeCanAnywhere(state: PermissionState) {
  return (permission: Permission): boolean => {
    if (state.loading) return false
    const permissions = state.permissions
    if (hasPermissions(permissions.organization, permission)) return true
    if (Object.values(permissions.servers).some((list) => hasPermissions(list, permission))) {
      return true
    }
    for (const stacks of Object.values(permissions.stacks)) {
      if (Object.values(stacks).some((list) => hasPermissions(list, permission))) return true
    }
    for (const containers of Object.values(permissions.containers)) {
      if (Object.values(containers).some((list) => hasPermissions(list, permission))) return true
    }
    return false
  }
}

function makeCanAnyOnServer(state: PermissionState) {
  return (serverId: string): boolean => {
    if (state.loading) return false
    const permissions = state.permissions
    if ((permissions.servers[serverId]?.length ?? 0) > 0) return true
    if (Object.keys(permissions.stacks[serverId] ?? {}).length > 0) return true
    if (Object.keys(permissions.containers[serverId] ?? {}).length > 0) return true
    return permissions.organization.some(
      (permission) =>
        permission === 'server.view' ||
        permission === 'stack.view' ||
        permission === 'container.view',
    )
  }
}

function makeCanSeeStacks(state: PermissionState, can: PermissionsContextValue['can']) {
  return (serverId: string): boolean => {
    if (state.loading) return false
    if (can('stack.view', { serverId })) return true
    return Object.keys(state.permissions.stacks[serverId] ?? {}).length > 0
  }
}

function makeCanSeeContainers(state: PermissionState, can: PermissionsContextValue['can']) {
  return (serverId: string): boolean => {
    if (state.loading) return false
    if (can('container.view', { serverId })) return true
    if (Object.keys(state.permissions.containers[serverId] ?? {}).length > 0) return true
    return Object.values(state.permissions.stacks[serverId] ?? {}).some((list) =>
      hasPermissions(list, 'container.view'),
    )
  }
}

/**
 * A component rendered outside the gate is an isolated unit (the screen tests
 * do this). It keeps the historical behaviour; the real panel always mounts the
 * provider, which denies while loading or after a failed /me.
 */
const permissiveContext: PermissionsContextValue = {
  loading: false,
  user: null,
  can: () => true,
  canAnywhere: () => true,
  canAnyOnServer: () => true,
  canSeeStacks: () => true,
  canSeeContainers: () => true,
}

/** PermissionsProvider fetches /me once for the signed-in user. */
export function PermissionsProvider({ meApi, children }: { meApi?: MeApi; children: ReactNode }) {
  const client = useMemo(() => meApi ?? createMeApi(), [meApi])
  const [state, setState] = useState<PermissionState>({
    loading: true,
    user: null,
    permissions: emptyPermissions,
  })

  useEffect(() => {
    let active = true
    client.get().then(
      (response) => {
        if (!active) return
        setState({
          loading: false,
          user: response.user,
          permissions: {
            organization: response.permissions?.organization ?? [],
            servers: response.permissions?.servers ?? {},
            stacks: response.permissions?.stacks ?? {},
            containers: response.permissions?.containers ?? {},
          },
        })
      },
      () => {
        if (active) {
          setState({ loading: false, user: null, permissions: emptyPermissions })
        }
      },
    )
    return () => {
      active = false
    }
  }, [client])

  const can = useCallback(
    (permission: Permission, scope?: PermissionScope) => {
      return makeCan(state)(permission, scope)
    },
    [state],
  )
  const canAnywhere = useCallback(
    (permission: Permission) => {
      return makeCanAnywhere(state)(permission)
    },
    [state],
  )
  const canAnyOnServer = useCallback(
    (serverId: string) => {
      return makeCanAnyOnServer(state)(serverId)
    },
    [state],
  )
  const canSeeStacks = useCallback(
    (serverId: string) => {
      return makeCanSeeStacks(state, makeCan(state))(serverId)
    },
    [state],
  )
  const canSeeContainers = useCallback(
    (serverId: string) => {
      return makeCanSeeContainers(state, makeCan(state))(serverId)
    },
    [state],
  )

  const value = useMemo<PermissionsContextValue>(
    () => ({
      loading: state.loading,
      user: state.user,
      can,
      canAnywhere,
      canAnyOnServer,
      canSeeStacks,
      canSeeContainers,
    }),
    [can, canAnywhere, canAnyOnServer, canSeeContainers, canSeeStacks, state.loading, state.user],
  )

  return <PermissionsContext.Provider value={value}>{children}</PermissionsContext.Provider>
}

/** usePermissions returns the effective permissions of the signed-in user. */
export function usePermissions(): PermissionsContextValue {
  return useContext(PermissionsContext) ?? permissiveContext
}
