import { apiClient } from './client'

export async function getMarkdownPage(slug: string): Promise<string> {
  const { data } = await apiClient.get<string>(`/pages/${encodeURIComponent(slug)}`, {
    responseType: 'text'
  })
  return data
}

export default {
  getMarkdownPage
}
