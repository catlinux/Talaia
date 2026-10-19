import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

export interface RecoveryCodesProps {
  codes: string[]
  onContinue: () => void
}

/** RecoveryCodes shows the one-time codes and asks for a confirmation. */
export function RecoveryCodes({ codes, onContinue }: RecoveryCodesProps) {
  const { t } = useTranslation('auth')
  const [confirmed, setConfirmed] = useState(false)
  const [status, setStatus] = useState<'idle' | 'copied' | 'failed'>('idle')
  const checkbox = useRef<HTMLInputElement>(null)

  useEffect(() => {
    checkbox.current?.focus()
  }, [])

  async function copy() {
    try {
      await navigator.clipboard.writeText(codes.join('\n'))
      setStatus('copied')
    } catch {
      setStatus('failed')
    }
  }

  function download() {
    const file = new Blob([`${codes.join('\n')}\n`], { type: 'text/plain' })
    const url = URL.createObjectURL(file)
    const link = document.createElement('a')
    link.href = url
    link.download = 'talaia-recovery-codes.txt'
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="auth">
      <div className="auth__card">
        <h1 className="auth__title">{t('recovery.title')}</h1>
        <p className="auth__intro">{t('recovery.intro')}</p>
        <p className="auth__warning">{t('recovery.once')}</p>
        <ul className="auth__codes">
          {codes.map((code) => (
            <li key={code}>
              <code>{code}</code>
            </li>
          ))}
        </ul>
        <div className="auth__actions">
          <button className="auth__button" type="button" onClick={() => void copy()}>
            {t('recovery.copy')}
          </button>
          <button className="auth__button" type="button" onClick={download}>
            {t('recovery.download')}
          </button>
        </div>
        {status !== 'idle' && (
          <p className={status === 'copied' ? 'auth__hint' : 'auth__error'} role="status">
            {status === 'copied' ? t('recovery.copied') : t('recovery.copyFailed')}
          </p>
        )}
        <label className="auth__check">
          <input
            ref={checkbox}
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
          />
          <span>{t('recovery.confirm')}</span>
        </label>
        <button className="auth__submit" type="button" disabled={!confirmed} onClick={onContinue}>
          {t('recovery.continue')}
        </button>
      </div>
    </div>
  )
}
