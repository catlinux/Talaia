#!/usr/bin/env node
// Pins the images of templates/*/template.yaml to the newest stable tag that
// still exists in its registry. Dry run by default; --write applies the changes.
//
//   node scripts/update-template-images.mjs [--write] [--major] [--only <template-id>]
//
// A candidate must have the same shape as the current tag (same prefix, same
// suffix, same number of numeric parts), so "17.2-alpine" never becomes
// "17.2" or "latest". The major version is kept unless --major is given (and
// always for databases: bumping it would leave existing data unreadable),
// because a new major usually needs changes in the template itself.
import { readdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'templates');
const args = process.argv.slice(2);
const write = args.includes('--write');
const only = args.includes('--only') ? args[args.indexOf('--only') + 1] : '';
const allowMajor = args.includes('--major');

const KEEP_MAJOR = ['postgres', 'mariadb', 'redis', 'clickhouse/clickhouse-server'];
const IMAGE_LINE = /^(\s*image:\s*)([^\s{][^\s]*)\s*$/;

// "ghcr.io/org/app:v1.2.3" -> registry, repository, tag
function parseRef(ref) {
  const i = ref.lastIndexOf(':');
  const name = ref.slice(0, i);
  const tag = ref.slice(i + 1);
  const parts = name.split('/');
  let registry = 'registry-1.docker.io';
  if (parts[0].includes('.') || parts[0].includes(':')) registry = parts.shift();
  let repo = parts.join('/');
  if (registry === 'registry-1.docker.io' && !repo.includes('/')) repo = `library/${repo}`;
  return { registry, repo, tag, name };
}

// prefix, numeric parts, suffix: "v2.1.4-x" -> ["v", [2,1,4], "-x"]
function shape(tag) {
  const m = tag.match(/^(\D*?)(\d+(?:\.\d+)*)(.*)$/);
  if (!m) return null;
  return { prefix: m[1], nums: m[2].split('.').map(Number), suffix: m[3] };
}

function compare(a, b) {
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const d = (a[i] ?? 0) - (b[i] ?? 0);
    if (d !== 0) return d;
  }
  return 0;
}

async function token(registry, header) {
  const realm = header.match(/realm="([^"]+)"/)?.[1];
  const service = header.match(/service="([^"]+)"/)?.[1];
  const scope = header.match(/scope="([^"]+)"/)?.[1];
  if (!realm) throw new Error(`no auth realm at ${registry}`);
  const u = new URL(realm);
  if (service) u.searchParams.set('service', service);
  if (scope) u.searchParams.set('scope', scope);
  const r = await fetch(u);
  if (!r.ok) throw new Error(`token ${registry}: ${r.status}`);
  const j = await r.json();
  return j.token ?? j.access_token;
}

async function listTags({ registry, repo }) {
  const tags = [];
  let url = `https://${registry}/v2/${repo}/tags/list?n=1000`;
  let auth = '';
  while (url) {
    let r = await fetch(url, { headers: auth ? { Authorization: `Bearer ${auth}` } : {} });
    if (r.status === 401 && !auth) {
      auth = await token(registry, r.headers.get('www-authenticate') ?? '');
      r = await fetch(url, { headers: { Authorization: `Bearer ${auth}` } });
    }
    if (!r.ok) throw new Error(`${registry}/${repo}: ${r.status}`);
    tags.push(...((await r.json()).tags ?? []));
    const next = r.headers.get('link')?.match(/<([^>]+)>;\s*rel="next"/)?.[1];
    url = next ? new URL(next, `https://${registry}`).href : '';
  }
  return tags;
}

const cache = new Map();
async function newest(ref) {
  const p = parseRef(ref);
  const cur = shape(p.tag);
  if (!cur) return null;
  if (!cache.has(p.name)) cache.set(p.name, await listTags(p));
  const keepMajor = !allowMajor || KEEP_MAJOR.includes(p.name);
  let best = { nums: cur.nums, tag: p.tag };
  let top = best;
  for (const t of cache.get(p.name)) {
    const s = shape(t);
    if (!s || s.prefix !== cur.prefix || s.suffix !== cur.suffix) continue;
    if (s.nums.length !== cur.nums.length) continue;
    if (compare(s.nums, top.nums) > 0) top = { nums: s.nums, tag: t };
    if (keepMajor && s.nums[0] !== cur.nums[0]) continue;
    if (compare(s.nums, best.nums) > 0) best = { nums: s.nums, tag: t };
  }
  return {
    ref: `${p.name}:${best.tag}`,
    exists: cache.get(p.name).includes(p.tag),
    newerMajor: top.nums[0] > best.nums[0] ? top.tag : '',
  };
}

let changed = 0;
let failed = 0;
for (const id of readdirSync(root).sort()) {
  const file = join(root, id, 'template.yaml');
  if ((only && id !== only) || !existsSync(file)) continue;
  const eol = readFileSync(file, 'utf8').includes('\r\n') ? '\r\n' : '\n';
  const lines = readFileSync(file, 'utf8').split(/\r?\n/);
  let dirty = false;
  for (let i = 0; i < lines.length; i++) {
    const m = lines[i].match(IMAGE_LINE);
    if (!m) continue;
    try {
      const res = await newest(m[2]);
      if (!res) {
        console.log(`${id}: ${m[2]} (no version to compare)`);
        continue;
      }
      if (!res.exists) console.log(`${id}: WARNING ${m[2]} no longer exists in the registry`);
      if (res.newerMajor) console.log(`${id}: note: newer major available (${res.newerMajor}), review the template before using --major`);
      if (res.ref !== m[2]) {
        console.log(`${id}: ${m[2]} -> ${res.ref}`);
        lines[i] = m[1] + res.ref;
        dirty = true;
        changed++;
      } else {
        console.log(`${id}: ${m[2]} (up to date)`);
      }
    } catch (e) {
      failed++;
      console.log(`${id}: ${m[2]} FAILED ${e.message}`);
    }
  }
  if (dirty && write) writeFileSync(file, lines.join(eol));
}
console.log(`${changed} image(s) ${write ? 'updated' : 'to update (dry run, use --write)'}, ${failed} failed`);
process.exit(failed ? 1 : 0);
