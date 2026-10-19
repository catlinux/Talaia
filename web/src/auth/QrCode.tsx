import { create } from 'qrcode'
import { useMemo } from 'react'

const QUIET_ZONE = 4

export interface QrCodeProps {
  value: string
  label: string
}

/**
 * QrCode draws the value as inline SVG. The panel's CSP only allows its own
 * origin, so an image from a data: URL would be blocked.
 */
export function QrCode({ value, label }: QrCodeProps) {
  const { size, path } = useMemo(() => {
    const { modules } = create(value, { errorCorrectionLevel: 'M' })
    let d = ''
    for (let row = 0; row < modules.size; row++) {
      for (let col = 0; col < modules.size; col++) {
        if (modules.get(row, col)) {
          d += `M${col + QUIET_ZONE} ${row + QUIET_ZONE}h1v1h-1z`
        }
      }
    }
    return { size: modules.size + QUIET_ZONE * 2, path: d }
  }, [value])

  return (
    <svg
      className="auth__qr"
      viewBox={`0 0 ${size} ${size}`}
      role="img"
      aria-label={label}
      shapeRendering="crispEdges"
    >
      <rect width={size} height={size} fill="#fff" />
      <path d={path} fill="#000" />
    </svg>
  )
}
