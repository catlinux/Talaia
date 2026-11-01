import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { onVersionMismatch } from '../../api/http'

interface UpdateBannerProps {
  /** reload is replaceable in tests; the panel uses window.location.reload. */
  reload?: () => void
}

/** UpdateBanner asks the user to reload after the core changed version. */
export function UpdateBanner({ reload = () => window.location.reload() }: UpdateBannerProps = {}) {
  const { t } = useTranslation('common')
  const [version, setVersion] = useState<string | null>(null)

  useEffect(() => onVersionMismatch(setVersion), [])

  if (version === null) return null
  return (
    <div className="shell__update" role="status">
      <span className="shell__update-text">{t('shell.updateAvailable', { version })}</span>
      <button type="button" className="shell__update-reload" onClick={reload}>
        {t('shell.reload')}
      </button>
    </div>
  )
}
