import { useTranslation } from 'react-i18next'

import logoMarkup from '../assets/logo.svg?raw'

/** Required attribution. It is a fixed credit and is never translated. */
export const ATTRIBUTION = 'Talaia — created by CatLinux'

const AUTHOR = 'CatLinux'
const LICENSE_ID = 'WNCL-1.0'
const LICENSE_URL = 'https://github.com/catlinux/Talaia'

export function About() {
  const { t } = useTranslation(['about', 'common'])

  return (
    <section className="about" aria-labelledby="about-title">
      <span
        className="about__logo"
        aria-hidden="true"
        dangerouslySetInnerHTML={{ __html: logoMarkup }}
      />
      <h1 className="about__title" id="about-title">
        {t('about:title')}
      </h1>
      <p className="about__description">{t('about:description')}</p>
      <p className="about__attribution">{ATTRIBUTION}</p>
      <dl className="about__facts">
        <dt>{t('about:project')}</dt>
        <dd>{t('common:appName')}</dd>
        <dt>{t('about:author')}</dt>
        <dd>{AUTHOR}</dd>
        <dt>{t('about:license')}</dt>
        <dd>
          <a href={LICENSE_URL}>{LICENSE_ID}</a>
        </dd>
        <dt>{t('about:version')}</dt>
        <dd>{__APP_VERSION__}</dd>
      </dl>
    </section>
  )
}
