import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Drawer } from '../components/ui'
import { WhatIsThis, type WhatIsThisTarget } from './WhatIsThis'

export interface CatalogDrawerValue {
  open: (target: WhatIsThisTarget) => void
}

const CatalogDrawerContext = createContext<CatalogDrawerValue | null>(null)

/** CatalogDrawerProvider renders the detailed catalogue card for the whole shell. */
export function CatalogDrawerProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation('catalog')
  const [target, setTarget] = useState<WhatIsThisTarget | null>(null)
  const open = useCallback((next: WhatIsThisTarget) => setTarget(next), [])
  const value = useMemo(() => ({ open }), [open])

  return (
    <CatalogDrawerContext.Provider value={value}>
      {children}
      <Drawer open={target !== null} title={t('label.details')} onClose={() => setTarget(null)}>
        {target !== null ? <WhatIsThis target={target} variant="full" /> : null}
      </Drawer>
    </CatalogDrawerContext.Provider>
  )
}

/** useCatalogDrawer returns the drawer opener, or null outside the provider. */
export function useCatalogDrawer(): CatalogDrawerValue | null {
  return useContext(CatalogDrawerContext)
}
