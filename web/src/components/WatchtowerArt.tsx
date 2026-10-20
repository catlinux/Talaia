interface WatchtowerArtProps {
  className?: string
}

/** WatchtowerArt is the decorative illustration of the empty fleet: a tower sending its beam. */
export function WatchtowerArt({ className }: WatchtowerArtProps) {
  return (
    <svg
      className={className}
      viewBox="0 0 96 96"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <circle cx="48" cy="30" r="22" strokeDasharray="2 6" opacity="0.45" />
      <circle cx="48" cy="30" r="34" strokeDasharray="2 8" opacity="0.22" />
      <path d="M40 84 43 42h10l3 42z" fill="color-mix(in srgb, currentColor 12%, transparent)" />
      <path d="M36 42h24v-8H36z" />
      <path d="M41 34v-6h14v6" />
      <path d="M48 24v-5" stroke="var(--signal)" />
      <path
        d="M33 30 20 24M63 30l13-6M31 38l-14 3M65 38l14 3"
        stroke="var(--signal)"
        opacity="0.85"
      />
      <path d="M32 84h32" />
      <path d="M46 54h4M45.4 64h5.2M44.8 74h6.4" opacity="0.6" />
    </svg>
  )
}
