import HttpUtils from '@/plugins/httputil'

export const fetchServerStatus = async (sections: string[]): Promise<Record<string, unknown> | undefined> => {
  const response = await HttpUtils.get('api/status', { r: sections.join(',') })
  return response.success ? response.obj as Record<string, unknown> : undefined
}

export const restartSingBox = async (): Promise<boolean> => {
  const response = await HttpUtils.post('api/restartSb', {})
  return response.success
}
