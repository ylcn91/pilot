import type { MetadataRoute } from 'next'
import { getPageMap } from 'nextra/page-map'

const BASE_URL = 'https://pilot.quantflow.studio'

type PageMapItem = {
  route?: string
  children?: PageMapItem[]
}

function collectRoutes(items: PageMapItem[], acc: Set<string>): void {
  for (const item of items) {
    if (typeof item.route === 'string' && !item.route.includes('[')) {
      acc.add(item.route)
    }
    if (Array.isArray(item.children)) {
      collectRoutes(item.children, acc)
    }
  }
}

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const pageMap = (await getPageMap()) as PageMapItem[]
  const routes = new Set<string>()
  collectRoutes(pageMap, routes)

  const lastModified = new Date()
  return [...routes].map((route) => ({
    url: `${BASE_URL}${route === '/' ? '' : route}`,
    lastModified,
  }))
}
