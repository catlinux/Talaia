import { useTranslation } from 'react-i18next'

import type { Container } from '../api/containers'
import type { ServiceUnit } from '../api/services'
import i18n from '../i18n'
import {
  imageRepository,
  lookupImage,
  lookupService,
  type CatalogCategory,
  type CatalogImportance,
} from './catalog'
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
  ifFails?: string
  origin: string
}

function text(key: string, options: Record<string, unknown> = {}): string {
  return i18n.t(key, { ns: 'catalog', ...options }) as string
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
      origin,
    }
  }
  return {
    known: true,
    id: entry.id,
    name: text(`entries.${entry.id}.name`),
    category: entry.category,
    importance: entry.importance,
    what: text(`entries.${entry.id}.what`),
    ifFails: text(`entries.${entry.id}.ifFails`),
    origin,
  }
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
      origin,
    }
  }
  return {
    known: true,
    id: entry.id,
    name: text(`entries.${entry.id}.name`),
    category: entry.category,
    importance: entry.importance,
    what: text(`entries.${entry.id}.what`),
    ifFails: text(`entries.${entry.id}.ifFails`),
    origin,
  }
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
      return {
        known: true,
        id: entry.id,
        name: text(`entries.${entry.id}.name`),
        category: entry.category,
        importance: entry.importance,
        what: text(`entries.${entry.id}.what`),
        ifFails: text(`entries.${entry.id}.ifFails`),
        origin: text('origin.stack', {
          stack: name,
          image: repositories[0],
        }),
      }
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
    origin,
  }
}

export type WhatIsThisTarget =
  | { kind: 'service'; unit: ServiceUnit }
  | { kind: 'container'; container: Container }
  | { kind: 'stack'; name: string; containers: Container[] }

interface WhatIsThisProps {
  target: WhatIsThisTarget
  variant?: 'compact' | 'full'
}

/** WhatIsThis renders the catalogue card used by the info tooltips. */
export function WhatIsThis({ target, variant = 'compact' }: WhatIsThisProps) {
  const { t } = useTranslation('catalog')
  const description =
    target.kind === 'service'
      ? describeService(target.unit)
      : target.kind === 'container'
        ? describeContainer(target.container)
        : describeStack(target.name, target.containers)

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
      {variant === 'full' && description.category !== undefined ? (
        <p className="whatisthis__help">{t(`categoryHelp.${description.category}`)}</p>
      ) : null}
      {variant === 'full' ? (
        <p className="whatisthis__help">{t(`importanceHelp.${description.importance}`)}</p>
      ) : null}
    </div>
  )
}
