import type { Deployment } from '../../api/apps'

/** formatDateTime formats an ISO instant for the active language. */
export function formatDateTime(value: string, language: string): string {
  if (value === '' || value.startsWith('0001-01-01')) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat(language, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

/** shortCommit keeps the first eight characters of a commit. */
export function shortCommit(commit: string): string {
  if (commit === '') return '—'
  return commit.slice(0, 8)
}

/** isActiveDeployment reports whether the deployment is still running. */
export function isActiveDeployment(deployment: Deployment): boolean {
  return (
    deployment.state === 'queued' ||
    deployment.state === 'building' ||
    deployment.state === 'releasing'
  )
}

/** deploymentDuration formats the elapsed time of a finished deployment. */
export function deploymentDuration(deployment: Deployment, language: string): string {
  if (
    deployment.startedAt === '' ||
    deployment.finishedAt === '' ||
    deployment.finishedAt.startsWith('0001-01-01')
  ) {
    return '—'
  }
  const start = new Date(deployment.startedAt).getTime()
  const finish = new Date(deployment.finishedAt).getTime()
  if (Number.isNaN(start) || Number.isNaN(finish) || finish < start) return '—'
  const seconds = Math.max(1, Math.round((finish - start) / 1000))
  const format = (value: number, unit: 'second' | 'minute') =>
    new Intl.NumberFormat(language, {
      style: 'unit',
      unit,
      unitDisplay: 'narrow',
      maximumFractionDigits: 0,
    }).format(value)
  if (seconds < 60) return format(seconds, 'second')
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  return `${format(minutes, 'minute')} ${format(rest, 'second')}`
}
