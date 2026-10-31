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
  /** checks are extra diagnostic commands, shown verbatim and not translated. */
  checks?: string[]
  /** docs is the official documentation page, when there is a well-known one. */
  docs?: string
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
    kind: 'both',
    category: 'security',
    importance: 'important',
    units: ['certbot'],
    images: ['certbot/certbot'],
    checks: ['certbot certificates', 'certbot renew --dry-run'],
  },
  {
    id: 'registry',
    kind: 'image',
    category: 'containers',
    importance: 'important',
    images: ['registry'],
  },
]

const docsById: Partial<Record<string, string>> = {
  ssh: 'https://www.openssh.com/manual.html',
  cron: 'https://man7.org/linux/man-pages/man8/cron.8.html',
  'systemd-journald': 'https://www.freedesktop.org/software/systemd/man/',
  'systemd-logind': 'https://www.freedesktop.org/software/systemd/man/',
  'systemd-networkd': 'https://www.freedesktop.org/software/systemd/man/',
  'systemd-resolved': 'https://www.freedesktop.org/software/systemd/man/',
  'systemd-timesyncd': 'https://www.freedesktop.org/software/systemd/man/',
  'systemd-udevd': 'https://www.freedesktop.org/software/systemd/man/',
  dbus: 'https://www.freedesktop.org/wiki/Software/dbus/',
  getty: 'https://www.freedesktop.org/software/systemd/man/',
  networking: 'https://wiki.debian.org/NetworkConfiguration',
  ifupdown: 'https://manpages.debian.org/ifup.8',
  rsyslog: 'https://www.rsyslog.com/doc/',
  chrony: 'https://chrony-project.org/documentation.html',
  ntp: 'https://www.ntp.org/documentation/',
  'unattended-upgrades': 'https://wiki.debian.org/UnattendedUpgrades',
  docker: 'https://docs.docker.com/engine/',
  containerd: 'https://containerd.io/docs/',
  apache2: 'https://httpd.apache.org/docs/',
  nginx: 'https://nginx.org/en/docs/',
  caddy: 'https://caddyserver.com/docs/',
  haproxy: 'https://docs.haproxy.org/',
  postfix: 'https://www.postfix.org/documentation.html',
  exim4: 'https://www.exim.org/docs.html',
  dovecot: 'https://doc.dovecot.org/',
  fail2ban: 'https://github.com/fail2ban/fail2ban/wiki',
  ufw: 'https://help.ubuntu.com/community/UFW',
  nftables: 'https://wiki.nftables.org/wiki-nftables/index.php/Main_Page',
  iptables: 'https://www.netfilter.org/documentation/',
  apparmor: 'https://apparmor.net/',
  auditd: 'https://github.com/linux-audit/audit-documentation/wiki',
  polkit: 'https://www.freedesktop.org/software/polkit/docs/latest/polkit.8.html',
  'qemu-guest-agent': 'https://www.qemu.org/docs/master/interop/qemu-ga-ref.html',
  'cloud-init': 'https://docs.cloud-init.io/',
  snapd: 'https://snapcraft.io/docs',
  modemmanager: 'https://www.freedesktop.org/wiki/Software/ModemManager/',
  'wpa-supplicant': 'https://w1.fi/wpa_supplicant/',
  avahi: 'https://avahi.org/',
  cups: 'https://openprinting.github.io/cups/',
  bluetooth: 'https://www.bluez.org/',
  mariadb: 'https://mariadb.com/kb/en/documentation/',
  mysql: 'https://dev.mysql.com/doc/',
  postgresql: 'https://www.postgresql.org/docs/',
  redis: 'https://redis.io/docs/',
  valkey: 'https://valkey.io/docs/',
  memcached: 'https://memcached.org/',
  mongodb: 'https://www.mongodb.com/docs/',
  rabbitmq: 'https://www.rabbitmq.com/docs',
  'php-fpm': 'https://www.php.net/manual/en/install.fpm.php',
  wireguard: 'https://www.wireguard.com/quickstart/',
  tailscale: 'https://tailscale.com/kb/',
  openvpn: 'https://openvpn.net/community-resources/',
  bind9: 'https://bind9.readthedocs.io/',
  unbound: 'https://unbound.docs.nlnetlabs.nl/',
  dnsmasq: 'https://thekelleys.org.uk/dnsmasq/doc.html',
  smartd: 'https://www.smartmontools.org/',
  lvm2: 'https://sourceware.org/lvm2/',
  mdmonitor: 'https://raid.wiki.kernel.org/',
  zfs: 'https://openzfs.github.io/openzfs-docs/',
  httpd: 'https://httpd.apache.org/docs/',
  traefik: 'https://doc.traefik.io/traefik/',
  'nginx-proxy-manager': 'https://nginxproxymanager.com/',
  portainer: 'https://docs.portainer.io/',
  'portainer-agent': 'https://docs.portainer.io/',
  watchtower: 'https://containrrr.dev/watchtower/',
  grafana: 'https://grafana.com/docs/',
  prometheus: 'https://prometheus.io/docs/',
  'node-exporter': 'https://github.com/prometheus/node_exporter',
  cadvisor: 'https://github.com/google/cadvisor',
  loki: 'https://grafana.com/docs/loki/latest/',
  promtail: 'https://grafana.com/docs/loki/latest/send-data/promtail/',
  wordpress: 'https://wordpress.org/documentation/',
  nextcloud: 'https://docs.nextcloud.com/',
  gitea: 'https://docs.gitea.com/',
  vaultwarden: 'https://github.com/dani-garcia/vaultwarden/wiki',
  minio: 'https://min.io/docs/minio/linux/index.html',
  adminer: 'https://www.adminer.org/',
  phpmyadmin: 'https://www.phpmyadmin.net/docs/',
  pgadmin: 'https://www.pgadmin.org/docs/',
  'uptime-kuma': 'https://github.com/louislam/uptime-kuma/wiki',
  pihole: 'https://docs.pi-hole.net/',
  adguard: 'https://adguard-dns.io/kb/',
  certbot: 'https://eff-certbot.readthedocs.io/',
  registry: 'https://distribution.github.io/distribution/',
}

for (const entry of catalog) {
  const docs = docsById[entry.id]
  if (docs !== undefined) entry.docs = docs
}

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
