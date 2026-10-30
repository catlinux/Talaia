import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  type Ref,
} from 'react'
import { useTranslation } from 'react-i18next'

export interface MenuItem {
  id: string
  label: ReactNode
  icon?: ReactNode
  danger?: boolean
  disabled?: boolean
  onSelect: () => void
}

export interface MenuSeparator {
  separator: true
}

export interface MenuHeading {
  heading: string
}

export type MenuEntry = MenuItem | MenuSeparator | MenuHeading

export interface MenuTriggerProps {
  ref: Ref<HTMLButtonElement>
  type: 'button'
  'aria-haspopup': 'menu'
  'aria-expanded': boolean
  'aria-controls': string
  onClick: (event: ReactMouseEvent<HTMLButtonElement>) => void
  onKeyDown: (event: ReactKeyboardEvent<HTMLButtonElement>) => void
}

interface MenuProps {
  trigger: (props: MenuTriggerProps) => ReactNode
  items: MenuEntry[]
  align?: 'start' | 'end'
}

/** Menu is an accessible popover menu anchored to a caller-supplied trigger. */
export function Menu({ trigger, items, align = 'start' }: MenuProps) {
  const { t } = useTranslation('ui')
  const menuID = useId()
  const [open, setOpen] = useState(false)
  const wrapper = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const focusTrigger = useCallback(() => {
    const target = triggerRef.current ?? wrapper.current?.querySelector<HTMLButtonElement>('button')
    target?.focus()
  }, [])

  const closeMenu = useCallback(() => {
    setOpen(false)
    focusTrigger()
  }, [focusTrigger])

  useEffect(() => {
    if (!open) return
    const onMouseDown = (event: MouseEvent) => {
      if (
        wrapper.current !== null &&
        event.target instanceof Node &&
        !wrapper.current.contains(event.target)
      ) {
        closeMenu()
      }
    }
    document.addEventListener('mousedown', onMouseDown)
    menuRef.current?.querySelector<HTMLElement>('[role="menuitem"]:not([disabled])')?.focus()
    return () => document.removeEventListener('mousedown', onMouseDown)
  }, [closeMenu, open])

  const triggerProps: MenuTriggerProps = {
    ref: triggerRef,
    type: 'button',
    'aria-haspopup': 'menu',
    'aria-expanded': open,
    'aria-controls': menuID,
    onClick: () => {
      if (open) closeMenu()
      else setOpen(true)
    },
    onKeyDown: (event) => {
      if (event.key === 'Escape' && open) {
        event.preventDefault()
        closeMenu()
        return
      }
      if (
        event.key === 'ArrowDown' ||
        event.key === 'ArrowUp' ||
        event.key === 'Enter' ||
        event.key === ' '
      ) {
        event.preventDefault()
        setOpen(true)
      }
    },
  }

  const menuItems = () =>
    Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ??
        [],
    )

  const onMenuKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      closeMenu()
      return
    }
    const entries = menuItems()
    if (entries.length === 0) return
    const current = entries.indexOf(document.activeElement as HTMLButtonElement)
    let next: number | undefined
    switch (event.key) {
      case 'ArrowDown':
        next = current < 0 ? 0 : (current + 1) % entries.length
        break
      case 'ArrowUp':
        next = current <= 0 ? entries.length - 1 : current - 1
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = entries.length - 1
        break
      case 'Enter':
      case ' ':
        if (current >= 0) {
          event.preventDefault()
          entries[current]!.click()
        }
        return
      default:
        return
    }
    event.preventDefault()
    entries[next]!.focus()
  }

  return (
    <div className="ui-menu" ref={wrapper}>
      {trigger(triggerProps)}
      {open ? (
        <div
          ref={menuRef}
          id={menuID}
          className={
            align === 'end' ? 'ui-menu__popover ui-menu__popover--end' : 'ui-menu__popover'
          }
          role="menu"
          aria-label={t('menu')}
          onKeyDown={onMenuKeyDown}
        >
          {items.map((item, index) =>
            'separator' in item ? (
              <div key={`separator-${index}`} className="ui-menu__separator" role="separator" />
            ) : 'heading' in item ? (
              <div key={`heading-${index}`} className="ui-menu__heading" role="presentation">
                {item.heading}
              </div>
            ) : (
              <button
                key={item.id}
                type="button"
                role="menuitem"
                tabIndex={-1}
                className={
                  item.danger === true ? 'ui-menu__item ui-menu__item--danger' : 'ui-menu__item'
                }
                disabled={item.disabled}
                onClick={() => {
                  if (item.disabled === true) return
                  item.onSelect()
                  closeMenu()
                }}
              >
                {item.icon !== undefined ? (
                  <span className="ui-menu__icon" aria-hidden="true">
                    {item.icon}
                  </span>
                ) : null}
                <span>{item.label}</span>
              </button>
            ),
          )}
        </div>
      ) : null}
    </div>
  )
}
