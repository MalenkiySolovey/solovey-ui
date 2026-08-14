import HttpUtils from '@/plugins/httputil'

export interface CertificateProbe {
  server: string
  port: number
  serverName: string
}

export const fetchServerStatus = async (sections: string[]): Promise<Record<string, unknown> | undefined> => {
  const response = await HttpUtils.get('api/status', { r: sections.join(',') })
  return response.success ? response.obj as Record<string, unknown> : undefined
}

export const restartSingBox = async (): Promise<boolean> => {
  const response = await HttpUtils.post('api/restartSb', {})
  return response.success
}

export const fetchCertificatePin = async (probe: CertificateProbe): Promise<string | undefined> => {
  const response = await HttpUtils.post('api/getCertPing', probe)
  const leafHash = response.obj?.leafHash
  return response.success && typeof leafHash === 'string' && leafHash.length > 0
    ? leafHash
    : undefined
}
