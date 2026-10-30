import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import { CloseIcon } from '../icons'

export type ToastKind = 'success' | 'error' | 'info'

interface ToastRecord {
  id: number
  kind: ToastKind
  message: string
}

interface ToastApi {
  success: (message: string) => void
  error: (message: string) => void
  info: (message: string) => void
}

const ToastContext = createContext<ToastApi | null>(null)

/** ToastProvider renders the stack of temporary notifications. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([])
  const nextID = useRef(0)

  const push = useCallback((kind: ToastKind, message: string) => {
    nextID.current += 1
    setToasts((current) => [...current, { id: nextID.current, kind, message }])
  }, [])

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id))
  }, [])

  const api = useMemo<ToastApi>(
    () => ({
      success: (message) => push('success', message),
      error: (message) => push('error', message),
      info: (message) => push('info', message),
    }),
    [push],
  )

  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className="ui-toast-region" aria-live="polite" aria-atomic="false">
        {toasts.map((toast) => (
          <ToastItem key={toast.id} toast={toast} onDismiss={dismiss} />
        ))}
      </div>
    </ToastContext.Provider>
  )
}

function ToastItem({ toast, onDismiss }: { toast: ToastRecord; onDismiss: (id: number) => void }) {
  const { t } = useTranslation('ui')
  useEffect(() => {
    const timer = window.setTimeout(() => onDismiss(toast.id), toast.kind === 'error' ? 8000 : 5000)
    return () => window.clearTimeout(timer)
  }, [onDismiss, toast.id, toast.kind])

  return (
    <div
      className={`ui-toast ui-toast--${toast.kind}`}
      role={toast.kind === 'error' ? 'alert' : undefined}
    >
      <span className="ui-toast__text">{toast.message}</span>
      <button
        type="button"
        className="ui-toast__close"
        aria-label={t('close')}
        onClick={() => onDismiss(toast.id)}
      >
        <CloseIcon />
      </button>
    </div>
  )
}

/** useToast returns the notification API of the surrounding provider. */
export function useToast(): ToastApi {
  const api = useContext(ToastContext)
  if (api === null) throw new Error('useToast must be used inside ToastProvider')
  return api
}
