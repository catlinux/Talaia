/** Catalog is the built-in "What is this?" reference of the panel. */

export type CatalogKind = 'service' | 'image' | 'both'

export type CatalogCategory =
  | 'system'
  | 'access'
  | 'security'
  | 'network'
  | 'web'
  | 'database'
  | 'cache'
  | 'queue'
  | 'monitoring'
  | 'backup'
  | 'containers'
  | 'mail'
  | 'dns'
  | 'time'
  | 'logs'
  | 'updates'
  | 'talaia'
  | 'app'

export type CatalogImportance = 'essential' | 'important' | 'optional'

export interface CatalogEntry {
  id: string
  kind: CatalogKind
  category: CatalogCategory
  importance: CatalogImportance
  /** units are exact systemd unit names, without the .service suffix. */
  units?: string[]
  /** unitPatterns match the unit name without its suffix and with template instances. */
  unitPatterns?: RegExp[]
  /** images are repository names without registry, library/ prefix or tag. */
  images?: string[]
}

export const catalog: CatalogEntry[] = [
  {
    id: 'ssh',
    kind: 'service',
    category: 'access',
    importance: 'essential',
    units: ['ssh', 'sshd'],
  },
  { id: 'cron', kind: 'service', category: 'system', importance: 'important', units: ['cron'] },
  {
    id: 'systemd-journald',
    kind: 'service',
    category: 'logs',
    importance: 'essential',
    units: ['systemd-journald'],
  },
  {
    id: 'systemd-logind',
    kind: 'service',
    category: 'system',
    importance: 'important',
    units: ['systemd-logind'],
  },
  {
    id: 'systemd-networkd',
    kind: 'service',
    category: 'network',
    importance: 'essential',
    units: ['systemd-networkd'],
  },
  {
    id: 'systemd-resolved',
    kind: 'service',
    category: 'dns',
    importance: 'important',
    units: ['systemd-resolved'],
  },
  {
    id: 'systemd-timesyncd',
    kind: 'service',
    category: 'time',
    importance: 'optional',
    units: ['systemd-timesyncd'],
  },
  {
    id: 'systemd-udevd',
    kind: 'service',
    category: 'system',
    importance: 'essential',
    units: ['systemd-udevd'],
  },
  { id: 'dbus', kind: 'service', category: 'system', importance: 'essential', units: ['dbus'] },
  {
    id: 'getty',
    kind: 'service',
    category: 'system',
    importance: 'optional',
    unitPatterns: [/^getty@/],
  },
  {
    id: 'networking',
    kind: 'service',
    category: 'network',
    importance: 'essential',
    units: ['networking'],
  },
  {
    id: 'ifupdown',
    kind: 'service',
    category: 'network',
    importance: 'essential',
    unitPatterns: [/^ifup@/],
  },
  { id: 'rsyslog', kind: 'service', category: 'logs', importance: 'optional', units: ['rsyslog'] },
  { id: 'chrony', kind: 'service', category: 'time', importance: 'optional', units: ['chrony'] },
  {
    id: 'ntp',
    kind: 'service',
    category: 'time',
    importance: 'optional',
    units: ['ntp', 'ntpsec'],
  },
  {
    id: 'unattended-upgrades',
    kind: 'service',
    category: 'updates',
    importance: 'important',
    units: ['unattended-upgrades'],
  },
  {
    id: 'apt-daily',
    kind: 'service',
    category: 'updates',
    importance: 'important',
    units: ['apt-daily', 'apt-daily-upgrade'],
    unitPatterns: [/^apt-daily/],
  },
  {
    id: 'docker',
    kind: 'service',
    category: 'containers',
    importance: 'essential',
    units: ['docker'],
  },
  {
    id: 'containerd',
    kind: 'service',
    category: 'containers',
    importance: 'essential',
    units: ['containerd'],
  },
  {
    id: 'apache2',
    kind: 'service',
    category: 'web',
    importance: 'important',
    units: ['apache2'],
  },
  {
    id: 'nginx',
    kind: 'both',
    category: 'web',
    importance: 'important',
    units: ['nginx'],
    images: ['nginx'],
  },
  {
    id: 'caddy',
    kind: 'both',
    category: 'web',
    importance: 'important',
    units: ['caddy'],
    images: ['caddy'],
  },
  {
    id: 'haproxy',
    kind: 'both',
    category: 'network',
    importance: 'important',
    units: ['haproxy'],
    images: ['haproxy'],
  },
  {
    id: 'postfix',
    kind: 'service',
    category: 'mail',
    importance: 'important',
    units: ['postfix'],
  },
  {
    id: 'exim4',
    kind: 'service',
    category: 'mail',
    importance: 'important',
    units: ['exim4'],
  },
  {
    id: 'dovecot',
    kind: 'service',
    category: 'mail',
    importance: 'important',
    units: ['dovecot'],
  },
  {
    id: 'fail2ban',
    kind: 'service',
    category: 'security',
    importance: 'important',
    units: ['fail2ban'],
  },
  { id: 'ufw', kind: 'service', category: 'security', importance: 'important', units: ['ufw'] },
  {
    id: 'nftables',
    kind: 'service',
    category: 'security',
    importance: 'important',
    units: ['nftables'],
  },
  {
    id: 'iptables',
    kind: 'service',
    category: 'security',
    importance: 'important',
    units: ['iptables', 'netfilter-persistent'],
  },
  {
    id: 'apparmor',
    kind: 'service',
    category: 'security',
    importance: 'important',
    units: ['apparmor'],
  },
  {
    id: 'auditd',
    kind: 'service',
    category: 'security',
    importance: 'important',
    units: ['auditd'],
  },
  {
    id: 'polkit',
    kind: 'service',
    category: 'security',
    importance: 'important',
    units: ['polkit'],
  },
  {
    id: 'qemu-guest-agent',
    kind: 'service',
    category: 'system',
    importance: 'optional',
    units: ['qemu-guest-agent'],
  },
  {
    id: 'cloud-init',
    kind: 'service',
    category: 'system',
    importance: 'important',
    unitPatterns: [/^cloud-/],
  },
  { id: 'snapd', kind: 'service', category: 'system', importance: 'optional', units: ['snapd'] },
  {
    id: 'modemmanager',
    kind: 'service',
    category: 'network',
    importance: 'optional',
    units: ['modemmanager'],
  },
  {
    id: 'wpa-supplicant',
    kind: 'service',
    category: 'network',
    importance: 'optional',
    units: ['wpa_supplicant'],
  },
  {
    id: 'avahi',
    kind: 'service',
    category: 'network',
    importance: 'optional',
    units: ['avahi-daemon'],
  },
  { id: 'cups', kind: 'service', category: 'system', importance: 'optional', units: ['cups'] },
  {
    id: 'bluetooth',
    kind: 'service',
    category: 'system',
    importance: 'optional',
    units: ['bluetooth'],
  },
  {
    id: 'mariadb',
    kind: 'both',
    category: 'database',
    importance: 'important',
    units: ['mariadb', 'mysql'],
    images: ['mariadb', 'bitnami/mariadb'],
  },
  {
    id: 'mysql',
    kind: 'image',
    category: 'database',
    importance: 'important',
    images: ['mysql', 'bitnami/mysql'],
  },
  {
    id: 'postgresql',
    kind: 'both',
    category: 'database',
    importance: 'important',
    unitPatterns: [/^postgresql/],
    images: ['postgres', 'bitnami/postgresql'],
  },
  {
    id: 'redis',
    kind: 'both',
    category: 'cache',
    importance: 'important',
    units: ['redis-server'],
    images: ['redis', 'bitnami/redis'],
  },
  {
    id: 'valkey',
    kind: 'image',
    category: 'cache',
    importance: 'important',
    images: ['valkey', 'valkey/valkey'],
  },
  {
    id: 'memcached',
    kind: 'both',
    category: 'cache',
    importance: 'important',
    units: ['memcached'],
    images: ['memcached'],
  },
  {
    id: 'mongodb',
    kind: 'image',
    category: 'database',
    importance: 'important',
    images: ['mongo', 'bitnami/mongodb'],
  },
  {
    id: 'rabbitmq',
    kind: 'image',
    category: 'queue',
    importance: 'important',
    images: ['rabbitmq'],
  },
  {
    id: 'php-fpm',
    kind: 'service',
    category: 'web',
    importance: 'important',
    unitPatterns: [/^php[0-9.]*-fpm$/],
  },
  {
    id: 'wireguard',
    kind: 'service',
    category: 'network',
    importance: 'important',
    unitPatterns: [/^wg-quick@/],
  },
  {
    id: 'tailscale',
    kind: 'service',
    category: 'network',
    importance: 'important',
    units: ['tailscaled'],
  },
  {
    id: 'openvpn',
    kind: 'service',
    category: 'network',
    importance: 'important',
    unitPatterns: [/^openvpn/],
  },
  {
    id: 'bind9',
    kind: 'service',
    category: 'dns',
    importance: 'important',
    units: ['bind9', 'named'],
  },
  {
    id: 'unbound',
    kind: 'service',
    category: 'dns',
    importance: 'important',
    units: ['unbound'],
  },
  {
    id: 'dnsmasq',
    kind: 'service',
    category: 'dns',
    importance: 'important',
    units: ['dnsmasq'],
  },
  {
    id: 'smartd',
    kind: 'service',
    category: 'monitoring',
    importance: 'optional',
    units: ['smartd', 'smartmontools'],
  },
  {
    id: 'lvm2',
    kind: 'service',
    category: 'system',
    importance: 'important',
    unitPatterns: [/^lvm2-/],
  },
  {
    id: 'mdmonitor',
    kind: 'service',
    category: 'system',
    importance: 'important',
    units: ['mdmonitor', 'mdadm'],
  },
  {
    id: 'zfs',
    kind: 'service',
    category: 'system',
    importance: 'important',
    unitPatterns: [/^zfs/],
  },
  {
    id: 'talaia-agent',
    kind: 'service',
    category: 'talaia',
    importance: 'essential',
    units: ['talaia-agent'],
  },
  {
    id: 'talaia-hostd',
    kind: 'service',
    category: 'talaia',
    importance: 'important',
    units: ['talaia-hostd'],
  },
  {
    id: 'talaia-core',
    kind: 'service',
    category: 'talaia',
    importance: 'essential',
    units: ['talaia-core'],
  },
  {
    id: 'httpd',
    kind: 'image',
    category: 'web',
    importance: 'important',
    images: ['httpd'],
  },
  {
    id: 'traefik',
    kind: 'image',
    category: 'network',
    importance: 'important',
    images: ['traefik'],
  },
  {
    id: 'nginx-proxy-manager',
    kind: 'image',
    category: 'web',
    importance: 'important',
    images: ['jc21/nginx-proxy-manager'],
  },
  {
    id: 'portainer',
    kind: 'image',
    category: 'containers',
    importance: 'important',
    images: ['portainer/portainer-ce'],
  },
  {
    id: 'portainer-agent',
    kind: 'image',
    category: 'containers',
    importance: 'important',
    images: ['portainer/agent'],
  },
  {
    id: 'watchtower',
    kind: 'image',
    category: 'containers',
    importance: 'optional',
    images: ['containrrr/watchtower'],
  },
  {
    id: 'grafana',
    kind: 'image',
    category: 'monitoring',
    importance: 'important',
    images: ['grafana/grafana'],
  },
  {
    id: 'prometheus',
    kind: 'image',
    category: 'monitoring',
    importance: 'important',
    images: ['prom/prometheus'],
  },
  {
    id: 'node-exporter',
    kind: 'image',
    category: 'monitoring',
    importance: 'important',
    images: ['prom/node-exporter'],
  },
  {
    id: 'cadvisor',
    kind: 'image',
    category: 'monitoring',
    importance: 'important',
    images: ['cadvisor', 'cadvisor/cadvisor'],
  },
  {
    id: 'loki',
    kind: 'image',
    category: 'logs',
    importance: 'important',
    images: ['grafana/loki'],
  },
  {
    id: 'promtail',
    kind: 'image',
    category: 'logs',
    importance: 'optional',
    images: ['grafana/promtail'],
  },
  {
    id: 'wordpress',
    kind: 'image',
    category: 'web',
    importance: 'important',
    images: ['wordpress'],
  },
  {
    id: 'nextcloud',
    kind: 'image',
    category: 'web',
    importance: 'important',
    images: ['nextcloud'],
  },
  {
    id: 'gitea',
    kind: 'image',
    category: 'web',
    importance: 'important',
    images: ['gitea/gitea'],
  },
  {
    id: 'vaultwarden',
    kind: 'image',
    category: 'security',
    importance: 'important',
    images: ['vaultwarden/server'],
  },
  {
    id: 'minio',
    kind: 'image',
    category: 'backup',
    importance: 'important',
    images: ['minio/minio'],
  },
  {
    id: 'adminer',
    kind: 'image',
    category: 'database',
    importance: 'optional',
    images: ['adminer'],
  },
  {
    id: 'phpmyadmin',
    kind: 'image',
    category: 'database',
    importance: 'optional',
    images: ['phpmyadmin'],
  },
  {
    id: 'pgadmin',
    kind: 'image',
    category: 'database',
    importance: 'optional',
    images: ['dpage/pgadmin4'],
  },
  {
    id: 'uptime-kuma',
    kind: 'image',
    category: 'monitoring',
    importance: 'optional',
    images: ['louislam/uptime-kuma'],
  },
  {
    id: 'pihole',
    kind: 'image',
    category: 'dns',
    importance: 'important',
    images: ['pihole/pihole'],
  },
  {
    id: 'adguard',
    kind: 'image',
    category: 'dns',
    importance: 'important',
    images: ['adguard/adguardhome'],
  },
  {
    id: 'certbot',
    kind: 'image',
    category: 'security',
    importance: 'important',
    images: ['certbot/certbot'],
  },
  {
    id: 'registry',
    kind: 'image',
    category: 'containers',
    importance: 'important',
    images: ['registry'],
  },
]

function normalizeUnit(unitName: string): { full: string; base: string } {
  const withoutSuffix = unitName
    .trim()
    .toLowerCase()
    .replace(/\.(service|timer|socket|target|path|mount|automount|swap|slice|scope)$/, '')
  const at = withoutSuffix.indexOf('@')
  return {
    full: withoutSuffix,
    base: at === -1 ? withoutSuffix : withoutSuffix.slice(0, at),
  }
}

/** lookupService finds a service entry by unit name, exact names first. */
export function lookupService(unitName: string): CatalogEntry | undefined {
  const { full, base } = normalizeUnit(unitName)
  const exact = catalog.find(
    (entry) =>
      entry.kind !== 'image' &&
      entry.units?.some((unit) => unit.toLowerCase() === full || unit.toLowerCase() === base),
  )
  if (exact !== undefined) return exact
  return catalog.find(
    (entry) =>
      entry.kind !== 'image' &&
      entry.unitPatterns?.some((pattern) => pattern.test(full) || pattern.test(base)),
  )
}

/** imageRepository strips the registry, library/ prefix, tag and digest of an image. */
export function imageRepository(image: string): string {
  let repository = image.trim().toLowerCase().split('@', 1)[0] ?? ''
  const lastSlash = repository.lastIndexOf('/')
  const lastColon = repository.lastIndexOf(':')
  if (lastColon > lastSlash) repository = repository.slice(0, lastColon)

  const firstSlash = repository.indexOf('/')
  if (firstSlash !== -1) {
    const first = repository.slice(0, firstSlash)
    if (first === 'localhost' || first.includes('.') || first.includes(':')) {
      repository = repository.slice(firstSlash + 1)
    }
  }
  if (repository.startsWith('library/')) repository = repository.slice('library/'.length)
  return repository
}

/** lookupImage finds an image entry by repository, ignoring registry and tag. */
export function lookupImage(image: string): CatalogEntry | undefined {
  const repository = imageRepository(image)
  return catalog.find(
    (entry) =>
      entry.kind !== 'service' && entry.images?.some((candidate) => candidate === repository),
  )
}
