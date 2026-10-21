import { useTranslation } from 'react-i18next'

import type { Server } from '../../api/types'
import { Meter } from './Meter'
import { formatNumber, formatPercent, formatRelativeTime } from './format'

interface ServerCardProps {
  server: Server
  /** onOpen opens the screen of the server; without it the name is plain text. */
  onOpen?: (server: Server) => void
}

export function ServerCard({ server, onOpen }: ServerCardProps) {
  const { t, i18n } = useTranslation('fleet')
  const language = i18n.resolvedLanguage ?? i18n.language

  const status = server.statusDetailKey
    ? t(server.statusDetailKey, { percent: formatPercent(server.diskPercent ?? 0, language) })
    : t(`status.${server.status}`)

  return (
    <li className={`server server--${server.status}`}>
      <div className="server__head">
        <h3 className="server__name">
          {onOpen ? (
            <button type="button" className="server__open" onClick={() => onOpen(server)}>
              {server.name}
            </button>
          ) : (
            server.name
          )}
        </h3>
        <span className={`status status--${server.status}`}>{status}</span>
      </div>
      {server.os !== '' ? (
        <p className="server__spec">
          {server.location === ''
            ? t('server.specNoLocation', {
                os: server.os,
                vcpu: formatNumber(server.vcpu, language),
                memory: formatNumber(server.memoryGb, language),
              })
            : t('server.spec', {
                os: server.os,
                location: server.location,
                vcpu: formatNumber(server.vcpu, language),
                memory: formatNumber(server.memoryGb, language),
              })}
        </p>
      ) : null}
      {server.agentVersion !== '' || server.agentUpdateAvailable || server.agentUpdateManual ? (
        <p className="server__agent">
          {server.agentVersion !== '' ? (
            <span>{t('server.agentVersion', { version: server.agentVersion })}</span>
          ) : null}
          {server.agentUpdateAvailable ? (
            <span className="server__update">{t('server.updateAvailable')}</span>
          ) : null}
          {server.agentUpdateManual ? (
            <span className="server__manual">{t('server.manualUpdate')}</span>
          ) : null}
        </p>
      ) : null}
      <div className="server__meters">
        <Meter label={t('metric.cpu')} percent={server.cpuPercent} />
        <Meter label={t('metric.ram')} percent={server.memPercent} />
        <Meter label={t('metric.disk')} percent={server.diskPercent} />
      </div>
      <div className="server__foot">
        <span>
          {t('server.facts', {
            containers: t('server.containers', { count: server.containers }),
            stacks: t('server.stacks', { count: server.stacks }),
          })}
        </span>
        <ul className="server__tags">
          {server.tags.map((tag) => (
            <li key={tag} className="tag">
              {tag}
            </li>
          ))}
        </ul>
      </div>
      {server.status === 'offline' && server.lastSeen ? (
        <p className="server__last-seen">
          {t('server.lastSeen', {
            time: formatRelativeTime(new Date(server.lastSeen), new Date(), language),
          })}
        </p>
      ) : null}
    </li>
  )
}
