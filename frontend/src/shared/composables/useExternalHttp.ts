import { fetchExternalJSON, probeExternalURL } from '@/plugins/httputil'

export const testExternalURL = (url: string): Promise<void> => probeExternalURL(url)
export const loadExternalJSON = (url: string): Promise<unknown> => fetchExternalJSON(url)
