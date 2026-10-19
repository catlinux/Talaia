import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { ActivityEvent } from '../../api/types'
import { formatBytes, formatNumber, formatPercent, formatRelativeTime } from './format'

interface ActivityFeedProps {
  events: ActivityEvent[]
}

// formatParams renders the message parameters: numbers go through Intl, and the
// parameters whose name ends in Bytes or Percent use the matching helper.
function formatParams(
  params: Record<string, string | number>,
  language: string,
): Record<string, string | number> {
  const formatted: Record<string, string | number> = {}

  for (const [key, value] of Object.entries(params)) {
    if (typeof value !== 'number') {
      formatted[key] = value
    } else if (key.endsWith('Bytes')) {
      formatted[key] = formatBytes(value, language)
    } else if (key.endsWith('Percent')) {
      formatted[key] = formatPercent(value, language)
    } else {
      formatted[key] = formatNumber(value, language)
    }
  }

  return formatted
}

export function ActivityFeed({ events }: ActivityFeedProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language

  // One reference instant for every relative time in the list.
  const now = useMemo(() => new Date(), [])

  return (
    <section className="activity" aria-labelledby="activity-title">
      <h2 className="activity__title" id="activity-title">
        {t('activity.title')} <span className="activity__live">{t('activity.live')}</span>
      </h2>
      <ul className="activity__list">
        {events.map((event) => (
          <li key={event.id} className={`activity__item activity__item--${event.severity}`}>
            <span className="activity__dot" aria-hidden="true" />
            <div className="activity__body">
              <p className="activity__message">
                {t(event.messageKey, formatParams(event.params, language))}
              </p>
              <p className="activity__meta">
                {t('activity.meta', {
                  time: formatRelativeTime(new Date(event.at), now, language),
                  source: t(event.source),
                })}
              </p>
            </div>
          </li>
        ))}
      </ul>
    </section>
  )
}
