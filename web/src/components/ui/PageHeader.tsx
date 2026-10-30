import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  subtitle?: string
  actions?: ReactNode
}

/** PageHeader is the title block shared by the new panel screens. */
export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <header className="ui-page-header">
      <div className="ui-page-header__titles">
        <h1 className="ui-page-header__title">{title}</h1>
        {subtitle !== undefined ? <p className="ui-page-header__subtitle">{subtitle}</p> : null}
      </div>
      {actions !== undefined ? <div className="ui-page-header__actions">{actions}</div> : null}
    </header>
  )
}
