import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { EnrollmentToken } from '../../api/types'
import { errorMessage } from '../../auth/AuthGate'

export interface AddServerProps {
  create: (name: string) => Promise<EnrollmentToken>
}

/** namePattern is what the core accepts as the name of a server. */
// \u002D is a hyphen: browsers compile the pattern in unicode-sets mode, where a bare one is not allowed.
const namePattern = String.raw`[A-Za-z0-9][A-Za-z0-9._\u002D]{0,62}`

/**
 * AddServer is the "Add server" button of the fleet screen: it asks for a name,
 * creates a one-time token and shows the command to run on the new server.
 */
export function AddServer({ create }: AddServerProps) {
  const { t } = useTranslation(['fleet', 'errors'])
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [created, setCreated] = useState<EnrollmentToken | null>(null)
  const [copied, setCopied] = useState(false)
  const nameField = useRef<HTMLInputElement>(null)

  function close() {
    setOpen(false)
    setName('')
    setError(null)
    setCreated(null)
    setCopied(false)
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setPending(true)
    setError(null)
    try {
      setCreated(await create(name.trim()))
    } catch (cause) {
      setError(errorMessage(t, cause))
    } finally {
      setPending(false)
    }
  }

  async function copy(command: string) {
    try {
      await navigator.clipboard.writeText(command)
      setCopied(true)
    } catch {
      // The clipboard can be blocked; the command stays selectable on screen.
      setCopied(false)
    }
  }

  if (!open) {
    return (
      <button
        className="addserver__open"
        type="button"
        onClick={() => {
          setOpen(true)
          // The field mounts on the next render.
          setTimeout(() => nameField.current?.focus(), 0)
        }}
      >
        {t('fleet:add.open')}
      </button>
    )
  }

  return (
    <section className="addserver" aria-labelledby="addserver-title">
      <h2 className="addserver__title" id="addserver-title">
        {t('fleet:add.title')}
      </h2>

      {created === null ? (
        <form onSubmit={submit}>
          <p className="addserver__hint">{t('fleet:add.intro')}</p>
          {error !== null && (
            <p className="addserver__error" role="alert">
              {error}
            </p>
          )}
          <label className="addserver__field">
            <span className="addserver__label">{t('fleet:add.nameLabel')}</span>
            <input
              ref={nameField}
              className="addserver__input"
              name="name"
              autoComplete="off"
              spellCheck={false}
              maxLength={63}
              pattern={namePattern}
              value={name}
              disabled={pending}
              required
              onChange={(event) => setName(event.target.value)}
            />
          </label>
          <p className="addserver__hint">{t('fleet:add.nameHint')}</p>
          <div className="addserver__actions">
            <button className="addserver__primary" type="submit" disabled={pending}>
              {pending ? t('fleet:add.creating') : t('fleet:add.create')}
            </button>
            <button className="addserver__secondary" type="button" onClick={close}>
              {t('fleet:add.cancel')}
            </button>
          </div>
        </form>
      ) : (
        <div>
          {created.command !== '' ? (
            <>
              <p className="addserver__hint">
                {t('fleet:add.runOnServer', { minutes: Math.round(created.expiresIn / 60) })}
              </p>
              <code className="addserver__command">{created.command}</code>
              <div className="addserver__actions">
                <button
                  className="addserver__primary"
                  type="button"
                  onClick={() => void copy(created.command)}
                >
                  {copied ? t('fleet:add.copied') : t('fleet:add.copy')}
                </button>
              </div>
            </>
          ) : (
            <>
              <p className="addserver__hint">{t('fleet:add.noAddress')}</p>
              <p className="addserver__label">{t('fleet:add.tokenLabel')}</p>
              <code className="addserver__command">{created.token}</code>
              <p className="addserver__label">{t('fleet:add.fingerprintLabel')}</p>
              <code className="addserver__command">{created.caFingerprint}</code>
            </>
          )}
          <p className="addserver__hint">{t('fleet:add.once')}</p>
          <div className="addserver__actions">
            <button className="addserver__secondary" type="button" onClick={close}>
              {t('fleet:add.done')}
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
