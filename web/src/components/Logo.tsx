interface LogoProps {
  className?: string
}

/**
 * Logo is the Talaia mark: a watchtower ("talaia") with its lantern lit and the
 * beams of its light. It uses the theme colours, so it follows light and dark.
 */
export function Logo({ className }: LogoProps) {
  return (
    <svg
      className={className}
      width="30"
      height="30"
      viewBox="0 0 32 32"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <rect width="32" height="32" rx="9" fill="var(--accent)" />
      <path d="M12.2 26.5 14 15.5h4l1.8 11z" fill="var(--accent-ink)" />
      <rect x="11.2" y="13.3" width="9.6" height="2.2" rx="0.9" fill="var(--accent-ink)" />
      <rect x="13.6" y="9" width="4.8" height="4.3" rx="1" fill="var(--accent-ink)" />
      <path d="M12.8 9.2 16 5.6l3.2 3.6z" fill="var(--accent-ink)" />
      <circle cx="16" cy="11.2" r="1.15" fill="var(--signal)" />
      <path
        d="M10.4 10.6 5.6 8.8M10.4 12.4 5.2 12.8M21.6 10.6l4.8-1.8M21.6 12.4l5.2.4"
        stroke="var(--signal)"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
      <path
        d="M10.4 26.5h11.2"
        stroke="var(--accent-ink)"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
    </svg>
  )
}
