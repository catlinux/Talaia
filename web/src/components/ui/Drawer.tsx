import { useEffect, useId, useRef, type CSSProperties, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { CloseIcon } from '../icons'

interface DrawerProps {
  open: boolean
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  width?: number | string
}

const focusableSelector =
  'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'

/** Drawer is a right-side modal panel with a focus trap and scroll lock. */
export function Drawer({ open, title, onClose, children, footer, width }: DrawerProps) {
  const { t } = useTranslation('ui')
  const titleID = useId()
  const panel = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)

  useEffect(() => {
    close.current = onClose
  }, [onClose])

  useEffect(() => {
    if (!open) return
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const node = panel.current
    node?.querySelector<HTMLElement>(focusableSelector)?.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        close.current()
        return
      }
      if (event.key !== 'Tab' || !node) return
      const items = Array.from(node.querySelectorAll<HTMLElement>(focusableSelector))
      if (items.length === 0) {
        event.preventDefault()
        return
      }
      const first = items[0]!
      const last = items[items.length - 1]!
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = overflow
      previous?.focus()
    }
  }, [open])

  if (!open) return null

  const style =
    width === undefined
      ? undefined
      : ({
          '--ui-drawer-width': typeof width === 'number' ? `${width}px` : width,
        } as CSSProperties)

  return (
    <div
      className="ui-drawer-overlay"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        ref={panel}
        className="ui-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        style={style}
      >
        <header className="ui-drawer__head">
          <h2 className="ui-drawer__title" id={titleID}>
            {title}
          </h2>
          <button
            type="button"
            className="ui-drawer__close"
            aria-label={t('close')}
            onClick={onClose}
          >
            <CloseIcon />
          </button>
        </header>
        <div className="ui-drawer__body">{children}</div>
        {footer !== undefined ? <footer className="ui-drawer__foot">{footer}</footer> : null}
      </div>
    </div>
  )
}
