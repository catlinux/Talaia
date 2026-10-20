import type { ReactNode } from 'react'

interface IconProps {
  className?: string
}

/** Icon draws a 24x24 stroke icon. Icons are decoration: the label is always text. */
function Icon({ className, children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      className={className}
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {children}
    </svg>
  )
}

export function FleetIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="3" y="4" width="18" height="6" rx="1.6" />
      <rect x="3" y="14" width="18" height="6" rx="1.6" />
      <path d="M7 7h.01M7 17h.01M11 7h6M11 17h6" />
    </Icon>
  )
}

export function InfoIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8h.01" />
    </Icon>
  )
}

export function SignOutIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M10 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h4M15 8l4 4-4 4M19 12H9" />
    </Icon>
  )
}

export function PlusIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M12 5v14M5 12h14" />
    </Icon>
  )
}

export function CopyIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="9" y="9" width="11" height="11" rx="2" />
      <path d="M5 15V6a2 2 0 0 1 2-2h9" />
    </Icon>
  )
}

export function CheckIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="m5 12.5 4.5 4.5L19 7.5" />
    </Icon>
  )
}

export function PlayIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M7 5v14l12-7z" />
    </Icon>
  )
}

export function StopIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <rect x="6" y="6" width="12" height="12" rx="2" />
    </Icon>
  )
}

export function RestartIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M20 12a8 8 0 1 1-2.6-5.9M20 4v4h-4" />
    </Icon>
  )
}

export function TrashIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v6M14 11v6" />
    </Icon>
  )
}

export function ArrowLeftIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M19 12H5M11 6l-6 6 6 6" />
    </Icon>
  )
}

export function LogsIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M6 3h9l4 4v14H6zM14 3v5h5M9 13h7M9 17h7M9 9h2" />
    </Icon>
  )
}

export function TerminalIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M3 5h18v14H3zM7 10l3 2-3 2M12 15h5" />
    </Icon>
  )
}

export function CloseIcon({ className }: IconProps) {
  return (
    <Icon className={className}>
      <path d="M6 6l12 12M18 6 6 18" />
    </Icon>
  )
}
