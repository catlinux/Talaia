import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { createAppsApi, type TemplateSummary, type WizardApi } from '../api/apps'
import { usePermissions } from '../auth/permissions'
import { FilterBar, PageHeader } from '../components/ui'
import { useNavigate } from '../router'
import './templates.css'

interface TemplatesProps {
  api?: WizardApi
}

/** Templates is the gallery of one-click apps. */
export function Templates({ api }: TemplatesProps) {
  const { t, i18n } = useTranslation('templates')
  const navigate = useNavigate()
  const { canAnywhere, loading: permissionsLoading } = usePermissions()
  const appsApi = useMemo(() => api ?? createAppsApi(), [api])
  const language = i18n.resolvedLanguage ?? i18n.language
  const alive = useRef(true)
  const [templates, setTemplates] = useState<TemplateSummary[] | null>(null)
  const [loadError, setLoadError] = useState(false)
  const [search, setSearch] = useState('')

  useEffect(() => {
    alive.current = true
    queueMicrotask(() => {
      appsApi.templates().then(
        (list) => {
          if (!alive.current) return
          setTemplates(list)
          setLoadError(false)
        },
        () => {
          if (alive.current) setLoadError(true)
        },
      )
    })
    return () => {
      alive.current = false
    }
  }, [appsApi])

  const canCreate = !permissionsLoading && canAnywhere('stack.edit')
  const normalized = search.trim().toLocaleLowerCase()
  const visible = (templates ?? []).filter((template) => {
    if (normalized === '') return true
    const name = template.names[language] ?? template.names.en ?? template.id
    const description = template.descriptions[language] ?? template.descriptions.en ?? ''
    return [name, template.id, description, template.image]
      .join(' ')
      .toLocaleLowerCase()
      .includes(normalized)
  })

  return (
    <section className="templates">
      <PageHeader title={t('title')} subtitle={t('subtitle', { count: templates?.length ?? 0 })} />
      <FilterBar
        search={{
          value: search,
          onChange: setSearch,
          placeholder: t('search'),
        }}
      />
      {templates === null && loadError ? (
        <div className="templates__error" role="alert">
          <p>{t('error')}</p>
          <button
            type="button"
            className="ui-btn"
            onClick={() => {
              setLoadError(false)
              setTemplates(null)
              queueMicrotask(() => {
                appsApi.templates().then(
                  (list) => {
                    if (alive.current) setTemplates(list)
                  },
                  () => {
                    if (alive.current) setLoadError(true)
                  },
                )
              })
            }}
          >
            {t('retry')}
          </button>
        </div>
      ) : templates === null ? (
        <div className="templates__grid" aria-busy="true">
          {Array.from({ length: 6 }, (_, index) => (
            <div className="templates__card templates__card--skeleton" key={index}>
              <span className="ui-table__skeleton" />
              <span className="ui-table__skeleton" />
              <span className="ui-table__skeleton" />
            </div>
          ))}
          <p className="ui-sr-only" role="status">
            {t('loading')}
          </p>
        </div>
      ) : visible.length === 0 ? (
        <div className="templates__empty">
          <h2>{search === '' ? t('emptyTitle') : t('noResultsTitle')}</h2>
          <p>{search === '' ? t('emptyBody') : t('noResultsBody')}</p>
        </div>
      ) : (
        <div className="templates__grid">
          {visible.map((template) => {
            const name = template.names[language] ?? template.names.en ?? template.id
            const description = template.descriptions[language] ?? template.descriptions.en ?? ''
            return (
              <article className="templates__card" key={template.id}>
                <div className="templates__card-head">
                  <h2>{name}</h2>
                  <span className="templates__id">{template.id}</span>
                </div>
                {description !== '' ? <p>{description}</p> : null}
                <dl className="templates__meta">
                  <div>
                    <dt>{t('image')}</dt>
                    <dd>{template.image}</dd>
                  </div>
                  <div>
                    <dt>{t('port')}</dt>
                    <dd>{template.port}</dd>
                  </div>
                  <div>
                    <dt>{t('ram')}</dt>
                    <dd>{t('ramValue', { ram: template.ramMB })}</dd>
                  </div>
                </dl>
                {canCreate ? (
                  <button
                    type="button"
                    className="ui-btn ui-btn--primary"
                    onClick={() =>
                      navigate(`/apps/new?template=${encodeURIComponent(template.id)}`)
                    }
                  >
                    {t('use')}
                  </button>
                ) : (
                  <p className="templates__muted">{t('requiresEdit')}</p>
                )}
              </article>
            )
          })}
        </div>
      )}
    </section>
  )
}
