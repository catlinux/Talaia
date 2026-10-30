import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

interface InfoTipProps {
  children: ReactNode
}

/** InfoTip is a small accessible info button with a tooltip that works by hover, focus and touch. */
export function InfoTip({ children }: InfoTipProps) {
  const { t } = useTranslation('ui')
  const tooltipID = useId()
  const wrapper = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const popover = useRef<HTMLDivElement>(null)
  const pinned = useRef(false)
  const [open, setOpen] = useState(false)

  const close = useCallback(() => {
    pinned.current = false
    setOpen(false)
  }, [])

  useEffect(() => {
    if (!open) return
    const onOutside = (event: MouseEvent) => {
      if (wrapper.current !== null && event.target instanceof Node) {
        if (!wrapper.current.contains(event.target)) close()
      }
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      close()
      trigger.current?.focus()
    }
    document.addEventListener('mousedown', onOutside)
    document.addEventListener('click', onOutside)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onOutside)
      document.removeEventListener('click', onOutside)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [close, open])

  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      const button = trigger.current
      const panel = popover.current
      if (button === null || panel === null) return
      const rect = button.getBoundingClientRect()
      const width = panel.offsetWidth
      const height = panel.offsetHeight
      const margin = 8
      const gap = 6
      let left = rect.left
      if (left + width > window.innerWidth - margin) {
        left = window.innerWidth - width - margin
      }
      left = Math.max(margin, left)
      let top = rect.bottom + gap
      if (top + height > window.innerHeight - margin) {
        top = rect.top - height - gap
      }
      top = Math.max(margin, top)
      panel.style.left = `${left}px`
      panel.style.top = `${top}px`
      panel.style.visibility = 'visible'
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open])

  const click = () => {
    if (open && pinned.current) {
      close()
      return
    }
    pinned.current = true
    setOpen(true)
  }

  const leave = (event: ReactMouseEvent<HTMLElement>) => {
    if (pinned.current) return
    const next = event.relatedTarget
    if (next instanceof Node && wrapper.current?.contains(next)) return
    if (wrapper.current?.contains(document.activeElement)) return
    setOpen(false)
  }

  return (
    <div
      ref={wrapper}
      className="info-tip"
      onMouseEnter={() => setOpen(true)}
      onMouseOver={() => setOpen(true)}
      onMouseLeave={leave}
      onBlur={(event) => {
        if (!wrapper.current?.contains(event.relatedTarget as Node | null)) close()
      }}
    >
      <button
        ref={trigger}
        type="button"
        className="info-tip__trigger"
        aria-label={t('moreInformation')}
        aria-describedby={open ? tooltipID : undefined}
        aria-expanded={open}
        onClick={click}
        onFocus={() => setOpen(true)}
        onMouseLeave={leave}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault()
            close()
          }
        }}
      >
        <span aria-hidden="true">i</span>
      </button>
      {open ? (
        <div
          ref={popover}
          id={tooltipID}
          className="info-tip__popover"
          role="tooltip"
          onMouseDown={(event) => event.stopPropagation()}
        >
          {children}
        </div>
      ) : null}
    </div>
  )
}
