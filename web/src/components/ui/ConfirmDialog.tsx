import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

interface ConfirmDialogProps {
  open: boolean
  title: string
  body: ReactNode
  confirmLabel: string
  danger?: boolean
  requireText?: string
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}

const focusableSelector =
  'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])'

/** ConfirmDialog is a modal confirmation with an optional typed gate. */
export function ConfirmDialog(props: ConfirmDialogProps) {
  if (!props.open) return null
  return <ConfirmDialogContent {...props} />
}

function ConfirmDialogContent({
  title,
  body,
  confirmLabel,
  danger = false,
  requireText,
  busy = false,
  onConfirm,
  onCancel,
}: Omit<ConfirmDialogProps, 'open'>) {
  const { t } = useTranslation('ui')
  const titleID = useId()
  const bodyID = useId()
  const inputID = useId()
  const panel = useRef<HTMLDivElement>(null)
  const cancel = useRef(onCancel)
  const isBusy = useRef(busy)
  const [typed, setTyped] = useState('')

  useEffect(() => {
    cancel.current = onCancel
  }, [onCancel])
  useEffect(() => {
    isBusy.current = busy
  }, [busy])

  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const overflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const node = panel.current
    node?.querySelector<HTMLElement>(focusableSelector)?.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!isBusy.current) cancel.current()
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
  }, [])

  const gated = requireText !== undefined && typed !== requireText
  return (
    <div
      className="ui-confirm-overlay"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busy) onCancel()
      }}
    >
      <div
        ref={panel}
        className="ui-confirm"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        aria-describedby={bodyID}
      >
        <h2 className="ui-confirm__title" id={titleID}>
          {title}
        </h2>
        <div className="ui-confirm__body" id={bodyID}>
          {body}
        </div>
        {requireText !== undefined ? (
          <label className="ui-confirm__field" htmlFor={inputID}>
            <span>{t('confirmText', { text: requireText })}</span>
            <input
              id={inputID}
              className="ui-confirm__input"
              value={typed}
              onChange={(event) => setTyped(event.target.value)}
            />
          </label>
        ) : null}
        <div className="ui-confirm__actions">
          <button type="button" className="ui-btn" disabled={busy} onClick={onCancel}>
            {t('cancel')}
          </button>
          <button
            type="button"
            className={danger ? 'ui-btn ui-btn--danger' : 'ui-btn ui-btn--primary'}
            disabled={busy || gated}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
