import { useEffect, useId, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { CloseIcon } from '../../components/icons'

interface DialogProps {
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  /** side opens the panel on the right instead of the centre. */
  side?: boolean
}

/**
 * Dialog is a labelled modal panel with a focus trap. Escape and the close
 * button close it; focus returns to the control that opened it.
 */
export function Dialog({ title, onClose, children, footer, side = false }: DialogProps) {
  const { t } = useTranslation('users')
  const titleID = useId()
  const panel = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  useEffect(() => {
    close.current = onClose
  }, [onClose])

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const node = panel.current
    const focusable = node?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href]',
    )
    focusable?.[0]?.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        close.current()
        return
      }
      if (event.key !== 'Tab' || !node) return
      const items = Array.from(
        node.querySelectorAll<HTMLElement>(
          'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href]',
        ),
      )
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
      previous?.focus()
    }
  }, [])

  return (
    <div
      className="dialog"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <div
        ref={panel}
        className={side ? 'dialog__panel dialog__panel--side' : 'dialog__panel'}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
      >
        <header className="dialog__head">
          <h2 className="dialog__title" id={titleID}>
            {title}
          </h2>
          <button
            type="button"
            className="dialog__close"
            aria-label={t('dialog.close')}
            onClick={onClose}
          >
            <CloseIcon />
          </button>
        </header>
        <div className="dialog__body">{children}</div>
        {footer ? <footer className="dialog__foot">{footer}</footer> : null}
      </div>
    </div>
  )
}
