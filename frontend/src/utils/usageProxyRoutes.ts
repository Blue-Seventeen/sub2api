import type { UsageProxyRoute } from '@/types'

type TranslateRoute = (key: string) => string

/** Share route labels between the administrator table and CSV export. */
export function formatUsageProxyRoutes(
  routes: UsageProxyRoute[] | null | undefined,
  t: TranslateRoute,
): string {
  const unknown = () => t('admin.usage.proxyRouteUnknown')
  if (!routes?.length) return unknown()
  return routes.map(route => {
    switch (route.kind) {
      case 'direct': return t('admin.usage.proxyRouteDirect')
      case 'system_proxy': return t('admin.usage.proxyRouteSystem')
      case 'proxy': {
        const host = route.host?.trim()
        if (!host || !route.port) return route.name || unknown()
        const formattedHost = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
        const address = `${formattedHost}:${route.port}`
        return route.name ? `${route.name} (${address})` : address
      }
      default: return unknown()
    }
  }).join(' → ')
}
