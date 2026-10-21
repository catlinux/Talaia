import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CheckIcon, CopyIcon } from '../../components/icons'

export interface ManualUpgradeProps {
  /** command upgrades the agent by hand; empty shows only the explanation. */
  command: string
}

/**
 * ManualUpgrade explains how to update an agent that is too old to update
 * itself and shows the command to run once on the server.
 */
export function ManualUpgrade({ command }: ManualUpgradeProps) {
  const { t } = useTranslation('server')
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
    } catch {
      // The clipboard can be blocked; the command stays selectable on screen.
      setCopied(false)
    }
  }

  return (
    <div className="manualupdate">
      <p className="manualupdate__text">{t('manualUpdate.explanation')}</p>
      {command !== '' ? (
        <>
          <p className="manualupdate__label">{t('manualUpdate.command')}</p>
          <code className="manualupdate__command">{command}</code>
          <div className="manualupdate__actions">
            <button type="button" className="manualupdate__copy" onClick={() => void copy()}>
              {copied ? <CheckIcon /> : <CopyIcon />}
              {copied ? t('manualUpdate.copied') : t('manualUpdate.copy')}
            </button>
          </div>
        </>
      ) : (
        <p className="manualupdate__docs">{t('manualUpdate.docs')}</p>
      )}
    </div>
  )
}
