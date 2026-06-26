import HttpUtils from '@/plugins/httputil'
import type { DoctorReport } from '@/types/doctor'

export interface DoctorRunResult {
  report?: DoctorReport
  error?: string
}

export const runConfigDoctor = async (): Promise<DoctorRunResult> => {
  const response = await HttpUtils.post('api/doctor/run', {})
  if (response.success) return { report: response.obj as DoctorReport }
  return { error: response.msg }
}
