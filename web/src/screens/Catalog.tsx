import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  catalog,
  type CatalogCategory,
  type CatalogEntry,
  type CatalogImportance,
} from '../catalog/catalog'
import { useCatalogDrawer } from '../catalog/CatalogDrawer'
import { DataTable, FilterBar, PageHeader, Select, type DataTableColumn } from '../components/ui'
import { setQuery, useRoute } from '../router'
import './catalog.css'

type TypeFilter = 'all' | 'service' | 'image'

interface CatalogRow {
  entry: CatalogEntry
  name: string
  units: string[]
  images: string[]
  search: string
}

const categories = [...new Set(catalog.map((entry) => entry.category))] as CatalogCategory[]
const importances: CatalogImportance[] = ['essential', 'important', 'optional']

function normalize(value: string): string {
  return value
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLocaleLowerCase()
}

function parseType(value: string | null): TypeFilter {
  return value === 'service' || value === 'image' ? value : 'all'
}

function parseCategory(value: string | null): CatalogCategory | '' {
  return categories.includes(value as CatalogCategory) ? (value as CatalogCategory) : ''
}

function parseImportance(value: string | null): CatalogImportance | '' {
  return importances.includes(value as CatalogImportance) ? (value as CatalogImportance) : ''
}

/** Catalog is the browsable reference of every built-in catalogue entry. */
export function Catalog() {
  const { t, i18n } = useTranslation('catalog')
  const { query } = useRoute()
  const drawer = useCatalogDrawer()
  const language = i18n.resolvedLanguage ?? i18n.language

  const rows = useMemo(() => {
    return catalog
      .map((entry) => {
        const name = t(`entries.${entry.id}.name`)
        const what = t(`entries.${entry.id}.what`)
        const units = (entry.units ?? []).map((unit) => `${unit}.service`)
        const images = entry.images ?? []
        return {
          entry,
          name,
          units,
          images,
          search: normalize([name, what, entry.id, ...units, ...images].join(' ')),
        }
      })
      .sort((left, right) => left.name.localeCompare(right.name, language))
  }, [language, t])

  const q = query.get('q') ?? ''
  const type = parseType(query.get('type'))
  const category = parseCategory(query.get('category'))
  const importance = parseImportance(query.get('importance'))
  const needle = normalize(q.trim())

  const visible = rows.filter((row) => {
    if (type === 'service' && row.entry.kind === 'image') return false
    if (type === 'image' && row.entry.kind === 'service') return false
    if (category !== '' && row.entry.category !== category) return false
    if (importance !== '' && row.entry.importance !== importance) return false
    return needle === '' || row.search.includes(needle)
  })

  const open = (entry: CatalogEntry) => {
    drawer?.open({ kind: 'entry', id: entry.id })
  }

  const columns: DataTableColumn<CatalogRow>[] = [
    {
      id: 'name',
      header: t('screen.columns.name'),
      cell: (row) => (
        <span className="catalog__name">
          <strong>{row.name}</strong>
          <span className="catalog__id">{row.entry.id}</span>
        </span>
      ),
    },
    {
      id: 'category',
      header: t('screen.columns.category'),
      cell: (row) => <span className="tag">{t(`category.${row.entry.category}`)}</span>,
    },
    {
      id: 'importance',
      header: t('screen.columns.importance'),
      cell: (row) => (
        <span
          className={`tag whatisthis__importance whatisthis__importance--${row.entry.importance}`}
        >
          {t(`importance.${row.entry.importance}`)}
        </span>
      ),
    },
    {
      id: 'type',
      header: t('screen.columns.type'),
      cell: (row) => <span className="tag catalog__type">{t(`kind.${row.entry.kind}`)}</span>,
    },
    {
      id: 'recognises',
      header: t('screen.columns.recognises'),
      hideOnMobile: true,
      cell: (row) => {
        const recognised = [...row.units, ...row.images]
        return (
          <span className="catalog__recognises">
            {recognised.length > 0 ? recognised.join(', ') : '—'}
          </span>
        )
      },
    },
    {
      id: 'actions',
      header: t('screen.columns.actions'),
      align: 'right',
      width: 112,
      cell: (row) => (
        <button type="button" className="ui-btn" onClick={() => open(row.entry)}>
          {t('label.viewCard')}
        </button>
      ),
    },
  ]

  return (
    <section className="catalog" aria-label={t('screen.title')}>
      <PageHeader title={t('screen.title')} subtitle={t('screen.subtitle')} />
      <FilterBar
        search={{
          value: q,
          onChange: (value) => setQuery({ q: value }),
          placeholder: t('screen.searchPlaceholder'),
        }}
        right={
          <span className="catalog__count" role="status">
            {t('screen.count', { count: visible.length })}
          </span>
        }
      >
        <Select
          label={t('screen.type')}
          value={type === 'all' ? '' : type}
          options={[
            { value: '', label: t('screen.typeAll') },
            { value: 'service', label: t('screen.typeServices') },
            { value: 'image', label: t('screen.typeImages') },
          ]}
          onChange={(value) => setQuery({ type: value === '' ? null : value })}
        />
        <Select
          label={t('screen.category')}
          value={category}
          options={[
            { value: '', label: t('screen.categoryAll') },
            ...categories.map((value) => ({
              value,
              label: t(`category.${value}`),
            })),
          ]}
          onChange={(value) => setQuery({ category: value === '' ? null : value })}
        />
        <Select
          label={t('screen.importance')}
          value={importance}
          options={[
            { value: '', label: t('screen.importanceAll') },
            ...importances.map((value) => ({
              value,
              label: t(`importance.${value}`),
            })),
          ]}
          onChange={(value) => setQuery({ importance: value === '' ? null : value })}
        />
      </FilterBar>
      <DataTable
        columns={columns}
        rows={visible}
        rowKey={(row) => row.entry.id}
        onRowClick={(row) => open(row.entry)}
        empty={<p className="catalog__empty">{t('screen.empty')}</p>}
      />
    </section>
  )
}
