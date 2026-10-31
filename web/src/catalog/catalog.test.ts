import { describe, expect, it } from 'vitest'

import i18n from '../i18n'
import { catalog, imageRepository, lookupImage, lookupService } from './catalog'

describe('lookupService', () => {
  it('normalizes the service suffix, case and template instances', () => {
    expect(lookupService('ssh.service')?.id).toBe('ssh')
    expect(lookupService('SSHD.SERVICE')?.id).toBe('ssh')
    expect(lookupService('getty@tty1.service')?.id).toBe('getty')
    expect(lookupService('getty@.service')?.id).toBe('getty')
    expect(lookupService('postgresql@16-main.service')?.id).toBe('postgresql')
    expect(lookupService('php8.2-fpm.service')?.id).toBe('php-fpm')
    expect(lookupService('wg-quick@wg0.service')?.id).toBe('wireguard')
    expect(lookupService('apt-daily.timer')?.id).toBe('apt-daily')
    expect(lookupService('certbot.service')?.id).toBe('certbot')
    expect(lookupService('certbot.timer')?.id).toBe('certbot')
  })

  it('matches the maintenance, boot and host-key units of this batch', () => {
    expect(lookupService('anacron.timer')?.id).toBe('anacron')
    expect(lookupService('logrotate.timer')?.id).toBe('logrotate')
    expect(lookupService('e2scrub_all.timer')?.id).toBe('e2scrub')
    expect(lookupService('getty-static.service')?.id).toBe('getty')
    expect(lookupService('serial-getty@ttyS0.service')?.id).toBe('getty')
    expect(lookupService('sshd@session-1.service')?.id).toBe('ssh')
    expect(lookupService('sshd-keygen.service')?.id).toBe('sshd-keygen')
    expect(lookupService('grub-common.service')?.id).toBe('grub')
    expect(lookupService('initrd-switch-root.service')?.id).toBe('initrd')
    expect(lookupService('rescue.service')?.id).toBe('rescue-mode')
    expect(lookupService('user@1000.service')?.id).toBe('user-session')
    expect(lookupService('user-runtime-dir@1000.service')?.id).toBe('user-session')
  })

  it('matches the grouped internal systemd units of this batch', () => {
    expect(lookupService('systemd-tmpfiles-setup.service')?.id).toBe('systemd-tmpfiles')
    expect(lookupService('systemd-fsck-root.service')?.id).toBe('systemd-fsck')
    expect(lookupService('systemd-quotacheck.service')?.id).toBe('systemd-fsck')
    expect(lookupService('systemd-oomd.service')?.id).toBe('systemd-oomd')
    expect(lookupService('systemd-hostnamed.service')?.id).toBe('systemd-hostnamed')
    expect(lookupService('systemd-timedated.service')?.id).toBe('systemd-hostnamed')
    expect(lookupService('systemd-modules-load.service')?.id).toBe('systemd-sysctl')
    expect(lookupService('modprobe@drm.service')?.id).toBe('systemd-sysctl')
    expect(lookupService('systemd-remount-fs.service')?.id).toBe('systemd-early-boot')
    expect(lookupService('systemd-journal-flush.service')?.id).toBe('systemd-early-boot')
    expect(lookupService('console-setup.service')?.id).toBe('console-setup')
    expect(lookupService('systemd-poweroff.service')?.id).toBe('systemd-power')
    expect(lookupService('systemd-ask-password-console.service')?.id).toBe('systemd-ask-password')
    expect(lookupService('systemd-networkd-wait-online.service')?.id).toBe('network-wait-online')
    expect(lookupService('NetworkManager-wait-online.service')?.id).toBe('network-wait-online')
  })

  it('matches the network, security, mail and time units of this batch', () => {
    expect(lookupService('NetworkManager.service')?.id).toBe('network-manager')
    expect(lookupService('NetworkManager-dispatcher.service')?.id).toBe('network-manager')
    expect(lookupService('connman.service')?.id).toBe('connman')
    expect(lookupService('firewalld.service')?.id).toBe('firewalld')
    expect(lookupService('openntpd.service')?.id).toBe('openntpd')
    expect(lookupService('nslcd.service')?.id).toBe('nslcd')
    expect(lookupService('opendkim.service')?.id).toBe('opendkim')
    expect(lookupService('ovs-vswitchd.service')?.id).toBe('openvswitch')
    expect(lookupService('netplan-ovs-cleanup.service')?.id).toBe('openvswitch')
    expect(lookupService('hv_kvp_daemon.service')?.id).toBe('hyperv-daemons')
    expect(lookupService('hv-vss-daemon.service')?.id).toBe('hyperv-daemons')
    expect(lookupService('ip6tables.service')?.id).toBe('iptables')
    expect(lookupService('ipset.service')?.id).toBe('iptables')
    expect(lookupService('chronyd.service')?.id).toBe('chrony')
    expect(lookupService('syslog.service')?.id).toBe('rsyslog')
    expect(lookupService('cups-browsed.service')?.id).toBe('cups')
    expect(lookupService('talaia-update.service')?.id).toBe('talaia-hostd')
  })

  it('keeps the exact systemd units of the existing entries', () => {
    for (const id of [
      'systemd-journald',
      'systemd-logind',
      'systemd-networkd',
      'systemd-resolved',
      'systemd-timesyncd',
      'systemd-udevd',
    ]) {
      expect(lookupService(`${id}.service`)?.id).toBe(id)
    }
  })

  it('prefers an exact unit over a pattern', () => {
    expect(lookupService('mysql.service')?.id).toBe('mariadb')
    expect(lookupService('docker.service')?.id).toBe('docker')
  })

  it('returns undefined for an unknown unit', () => {
    expect(lookupService('not-a-real-service.service')).toBeUndefined()
  })
})

describe('lookupImage', () => {
  it('ignores registries, library, tags and digests', () => {
    expect(lookupImage('docker.io/library/postgres:16-alpine')?.id).toBe('postgresql')
    expect(lookupImage('POSTGRES@sha256:abcd')?.id).toBe('postgresql')
    expect(lookupImage('registry.example.com:5000/library/redis:7')?.id).toBe('redis')
    expect(lookupImage('quay.io/library/mariadb')?.id).toBe('mariadb')
    expect(lookupImage('gcr.io/cadvisor:0.49.1')?.id).toBe('cadvisor')
    expect(lookupImage('gcr.io/cadvisor/cadvisor:0.49.1')?.id).toBe('cadvisor')
    expect(lookupImage('portainer/portainer-ce:latest')?.id).toBe('portainer')
  })

  it('returns undefined for unknown images', () => {
    expect(lookupImage('example.com/team/unknown:1')).toBeUndefined()
  })
})

describe('imageRepository', () => {
  it('keeps a repository without registry, tag or digest', () => {
    expect(imageRepository('docker.io/library/postgres:16')).toBe('postgres')
    expect(imageRepository('localhost:5000/team/app@sha256:abc')).toBe('team/app')
    expect(imageRepository('NGINX:latest')).toBe('nginx')
  })
})

describe('catalog translations', () => {
  it('has name, what and ifFails for every entry in ca, es and en', () => {
    for (const language of ['ca', 'es', 'en']) {
      const t = i18n.getFixedT(language, 'catalog')
      for (const entry of catalog) {
        for (const field of ['name', 'what', 'ifFails']) {
          const key = `entries.${entry.id}.${field}`
          expect(t(key), `${language} ${key}`).not.toBe(key)
        }
      }
      for (const category of new Set(catalog.map((entry) => entry.category))) {
        expect(t(`category.${category}`), `${language} category ${category}`).not.toBe(
          `category.${category}`,
        )
        expect(t(`categoryHelp.${category}`), `${language} category help ${category}`).not.toBe(
          `categoryHelp.${category}`,
        )
      }
      for (const importance of ['essential', 'important', 'optional', 'unknown']) {
        expect(t(`importance.${importance}`), `${language} importance ${importance}`).not.toBe(
          `importance.${importance}`,
        )
        expect(
          t(`importanceHelp.${importance}`),
          `${language} importance help ${importance}`,
        ).not.toBe(`importanceHelp.${importance}`)
      }
    }
  })

  it('has the detailed fields for the maintenance and boot entries', () => {
    const ids = [
      'anacron',
      'logrotate',
      'man-db',
      'dpkg-db-backup',
      'e2scrub',
      'fstrim',
      'phpsessionclean',
      'sshd-keygen',
      'uuidd',
      'rc-local',
      'grub',
      'rescue-mode',
      'initrd',
      'plymouth',
      'user-session',
      'getty',
      'systemd-tmpfiles',
      'systemd-fsck',
      'systemd-oomd',
      'systemd-hostnamed',
      'systemd-sysctl',
      'systemd-early-boot',
      'console-setup',
      'systemd-power',
      'systemd-ask-password',
      'network-wait-online',
      'network-manager',
      'connman',
      'firewalld',
      'openntpd',
      'nslcd',
      'opendkim',
      'openvswitch',
      'hyperv-daemons',
      'iptables',
      'chrony',
      'rsyslog',
      'cups',
      'talaia-hostd',
      'talaia-agent',
      'talaia-core',
    ]
    for (const language of ['ca', 'es', 'en']) {
      const t = i18n.getFixedT(language, 'catalog')
      for (const id of ids) {
        for (const field of ['name', 'what', 'ifFails', 'details', 'stop']) {
          const key = `entries.${id}.${field}`
          expect(t(key), `${language} ${key}`).not.toBe(key)
        }
        const problems = t(`entries.${id}.problems`, { returnObjects: true }) as unknown
        expect(Array.isArray(problems), `${language} ${id} problems`).toBe(true)
        expect((problems as string[]).length).toBeGreaterThanOrEqual(2)
      }
    }
  })

  it('has unique ids and non-overlapping exact units and images', () => {
    expect(new Set(catalog.map((entry) => entry.id)).size).toBe(catalog.length)
    const units = catalog.flatMap((entry) => entry.units ?? [])
    expect(new Set(units).size).toBe(units.length)
    const images = catalog.flatMap((entry) => entry.images ?? [])
    expect(new Set(images).size).toBe(images.length)
  })

  it('uses https documentation links and the Certbot diagnostics', () => {
    for (const entry of catalog) {
      if (entry.docs !== undefined) expect(entry.docs.startsWith('https://')).toBe(true)
    }
    const certbot = catalog.find((entry) => entry.id === 'certbot')
    expect(certbot?.kind).toBe('both')
    expect(certbot?.units).toEqual(['certbot'])
    expect(certbot?.checks).toEqual(['certbot certificates', 'certbot renew --dry-run'])
  })

  it('describes the Talaia update helper as the update path and service', () => {
    const hostd = catalog.find((entry) => entry.id === 'talaia-hostd')
    expect(hostd?.units).toEqual(['talaia-update', 'talaia-hostd'])
    expect(hostd?.unitPatterns).toBeUndefined()
    expect(hostd?.checks).toEqual([
      'systemctl status talaia-update.path',
      'journalctl -u talaia-update -n 100 --no-pager',
    ])
  })
})
