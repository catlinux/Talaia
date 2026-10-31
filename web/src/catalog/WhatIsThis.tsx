import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import type { Container } from '../api/containers'
import type { ServiceUnit } from '../api/services'
import i18n from '../i18n'
import {
  catalog,
  imageRepository,
  lookupImage,
  lookupService,
  type CatalogCategory,
  type CatalogEntry,
  type CatalogImportance,
} from './catalog'
import { useCatalogDrawer } from './CatalogDrawer'
import './whatisthis.css'

export type DescriptionImportance = CatalogImportance | 'unknown'

/** CatalogDescription is the text a "What is this?" card shows. */
export interface CatalogDescription {
  known: boolean
  id?: string
  name: string
  category?: CatalogCategory
  importance: DescriptionImportance
  what: string
  details?: string
  stop?: string
  ifFails?: string
  problems: string[]
  commands: string[]
  docs?: string
  origin: string
}

function text(key: string, options: Record<string, unknown> = {}): string {
  return i18n.t(key, { ns: 'catalog', ...options }) as string
}

function optionalText(key: string): string | undefined {
  if (!i18n.exists(key, { ns: 'catalog' })) return undefined
  return text(key)
}

function optionalProblems(key: string): string[] {
  if (!i18n.exists(key, { ns: 'catalog' })) return []
  const value: unknown = i18n.t(key, { ns: 'catalog', returnObjects: true })
  return Array.isArray(value) && value.every((item): item is string => typeof item === 'string')
    ? value
    : []
}

function serviceCommands(unitName: string): string[] {
  return [`systemctl status ${unitName}`, `journalctl -u ${unitName} -n 50 --no-pager`]
}

function containerCommands(name: string): string[] {
  return [`docker logs --tail 50 ${name}`, `docker inspect ${name}`]
}

function stackCommands(name: string): string[] {
  return [`docker compose -p ${name} ps`, `docker compose -p ${name} logs --tail 50`]
}

function entryDescription(
  entry: CatalogEntry,
  origin: string,
  commands: string[],
  // Entry checks are host commands: they do not apply to a container or a stack.
  withChecks = true,
): CatalogDescription {
  const base = `entries.${entry.id}`
  return {
    known: true,
    id: entry.id,
    name: text(`${base}.name`),
    category: entry.category,
    importance: entry.importance,
    what: text(`${base}.what`),
    details: optionalText(`${base}.details`),
    stop: optionalText(`${base}.stop`),
    ifFails: text(`${base}.ifFails`),
    problems: optionalProblems(`${base}.problems`),
    commands: withChecks ? [...commands, ...(entry.checks ?? [])] : commands,
    docs: entry.docs,
    origin,
  }
}

function serviceOrigin(unit: ServiceUnit): string {
  return text('origin.service', { unit: unit.name })
}

/** describeService returns the catalogue card of one systemd unit. */
export function describeService(unit: ServiceUnit): CatalogDescription {
  const entry = lookupService(unit.name)
  const origin = serviceOrigin(unit)
  if (entry === undefined) {
    const description = unit.description || text('unknownServiceDescription')
    return {
      known: false,
      name: unit.name,
      importance: 'unknown',
      what: text('unknownService', { description }),
      problems: [],
      commands: serviceCommands(unit.name),
      origin,
    }
  }
  return entryDescription(entry, origin, serviceCommands(unit.name))
}

function catalogOrigin(entry: CatalogEntry): string {
  const units = (entry.units ?? []).map((unit) => `${unit}.service`)
  const images = entry.images ?? []
  if (units.length > 0 && images.length > 0) {
    return text('origin.catalogBoth', {
      units: units.join(', '),
      images: images.join(', '),
    })
  }
  if (units.length > 0) return text('origin.catalogUnits', { units: units.join(', ') })
  if (images.length > 0) return text('origin.catalogImages', { images: images.join(', ') })
  return text('origin.catalogNone')
}

/** describeEntry returns the catalogue card of one catalogue entry. */
export function describeEntry(id: string): CatalogDescription {
  const entry = catalog.find((candidate) => candidate.id === id)
  if (entry === undefined) {
    return {
      known: false,
      name: id,
      importance: 'unknown',
      what: id,
      problems: [],
      commands: [],
      origin: id,
    }
  }
  const unit = entry.units?.[0]
  return entryDescription(
    entry,
    catalogOrigin(entry),
    unit === undefined ? [] : serviceCommands(`${unit}.service`),
  )
}

function containerOrigin(container: Container): string {
  const image = imageRepository(container.image) || container.image
  if (container.composeProject !== '') {
    if (container.composeService !== '') {
      return text('origin.stackService', {
        stack: container.composeProject,
        service: container.composeService,
        image,
      })
    }
    return text('origin.stack', { stack: container.composeProject, image })
  }
  return text('origin.standalone', { image })
}

/** describeContainer returns the catalogue card of one container image. */
export function describeContainer(container: Container): CatalogDescription {
  const entry = lookupImage(container.image)
  const origin = containerOrigin(container)
  if (entry === undefined) {
    return {
      known: false,
      name: container.name,
      importance: 'unknown',
      what: origin,
      problems: [],
      commands: containerCommands(container.name),
      origin,
    }
  }
  return entryDescription(entry, origin, containerCommands(container.name), false)
}

/**
 * describeStack uses the image catalogue only when every container of the
 * stack shares one known image. Otherwise the card explains where it comes from.
 */
export function describeStack(name: string, containers: Container[]): CatalogDescription {
  const repositories = [...new Set(containers.map((container) => imageRepository(container.image)))]
  if (repositories.length === 1) {
    const entry = lookupImage(repositories[0] ?? '')
    if (entry !== undefined) {
      return entryDescription(
        entry,
        text('origin.stack', {
          stack: name,
          image: repositories[0],
        }),
        stackCommands(name),
        false,
      )
    }
  }

  const origin =
    containers.length === 0
      ? text('origin.stackNoContainers', { stack: name })
      : text('origin.stackImages', {
          stack: name,
          images: repositories.filter(Boolean).join(', '),
        })
  return {
    known: false,
    name,
    importance: 'unknown',
    what: origin,
    problems: [],
    commands: stackCommands(name),
    origin,
  }
}

export type WhatIsThisTarget =
  | { kind: 'service'; unit: ServiceUnit }
  | { kind: 'container'; container: Container }
  | { kind: 'stack'; name: string; containers: Container[] }
  | { kind: 'entry'; id: string }

interface WhatIsThisProps {
  target: WhatIsThisTarget
  variant?: 'compact' | 'full'
}

/** WhatIsThis renders the catalogue card used by the info tooltips. */
export function WhatIsThis({ target, variant = 'compact' }: WhatIsThisProps) {
  const { t } = useTranslation('catalog')
  const drawer = useCatalogDrawer()
  const [copiedCommand, setCopiedCommand] = useState<string | null>(null)
  const description =
    target.kind === 'service'
      ? describeService(target.unit)
      : target.kind === 'container'
        ? describeContainer(target.container)
        : target.kind === 'entry'
          ? describeEntry(target.id)
          : describeStack(target.name, target.containers)
  const full = variant === 'full'

  const copy = (command: string) => {
    const clipboard = navigator.clipboard
    if (!clipboard) return
    void clipboard.writeText(command).then(
      () => setCopiedCommand(command),
      () => undefined,
    )
  }

  return (
    <div className={`whatisthis whatisthis--${variant}`}>
      <div className="whatisthis__head">
        <strong className="whatisthis__name">{description.name}</strong>
        <span className="whatisthis__tags">
          {description.category !== undefined ? (
            <span className="tag">{t(`category.${description.category}`)}</span>
          ) : null}
          <span
            className={`tag whatisthis__importance whatisthis__importance--${description.importance}`}
          >
            {t(`importance.${description.importance}`)}
          </span>
        </span>
      </div>

      {full ? (
        <>
          <section className="whatisthis__section">
            <h3 className="whatisthis__heading">{t('label.what')}</h3>
            <p className="whatisthis__what">{description.what}</p>
            {description.details !== undefined ? (
              <p className="whatisthis__what">{description.details}</p>
            ) : null}
          </section>

          {description.stop !== undefined ? (
            <section className="whatisthis__section">
              <h3 className="whatisthis__heading">{t('label.stop')}</h3>
              <p className="whatisthis__what">{description.stop}</p>
            </section>
          ) : null}

          {description.ifFails !== undefined || description.problems.length > 0 ? (
            <section className="whatisthis__section">
              <h3 className="whatisthis__heading">{t('label.ifFails')}</h3>
              {description.ifFails !== undefined ? (
                <p className="whatisthis__fail">{description.ifFails}</p>
              ) : null}
              {description.problems.length > 0 ? (
                <>
                  <h4 className="whatisthis__subheading">{t('label.problems')}</h4>
                  <ul className="whatisthis__problems">
                    {description.problems.map((problem) => (
                      <li key={problem}>{problem}</li>
                    ))}
                  </ul>
                </>
              ) : null}
            </section>
          ) : null}

          {description.commands.length > 0 ? (
            <section className="whatisthis__section">
              <h3 className="whatisthis__heading">{t('label.howToCheck')}</h3>
              <div className="whatisthis__commands">
                {description.commands.map((command) => (
                  <div className="whatisthis__command" key={command}>
                    <code>{command}</code>
                    <button
                      type="button"
                      className="whatisthis__copy"
                      onClick={() => copy(command)}
                    >
                      {copiedCommand === command ? t('label.copied') : t('label.copy')}
                    </button>
                  </div>
                ))}
              </div>
            </section>
          ) : null}

          <section className="whatisthis__section">
            <h3 className="whatisthis__heading">{t('label.origin')}</h3>
            <p className="whatisthis__origin">{description.origin}</p>
          </section>

          {description.docs !== undefined ? (
            <section className="whatisthis__section">
              <h3 className="whatisthis__heading">{t('label.docs')}</h3>
              <a
                className="whatisthis__docs"
                href={description.docs}
                target="_blank"
                rel="noopener noreferrer"
              >
                {description.docs}
              </a>
            </section>
          ) : null}

          {description.category !== undefined ? (
            <p className="whatisthis__help">{t(`categoryHelp.${description.category}`)}</p>
          ) : null}
          <p className="whatisthis__help">{t(`importanceHelp.${description.importance}`)}</p>
        </>
      ) : (
        <>
          <p className="whatisthis__what">{description.what}</p>
          {description.ifFails !== undefined ? (
            <p className="whatisthis__fail">
              <strong>{t('label.ifFails')}:</strong> {description.ifFails}
            </p>
          ) : null}
          {description.origin !== description.what ? (
            <p className="whatisthis__origin">
              <strong>{t('label.origin')}:</strong> {description.origin}
            </p>
          ) : null}
          {drawer !== null ? (
            <button type="button" className="whatisthis__open" onClick={() => drawer.open(target)}>
              {t('label.openFull')}
            </button>
          ) : null}
        </>
      )}
    </div>
  )
}
