export type ActionableLogLevel = 'warning' | 'error'

export const actionableLogLevel = (log: string): ActionableLogLevel | undefined => {
  if (/\b(?:ERROR|FATAL)\b/i.test(log)) return 'error'
  if (/\bWARN(?:ING)?\b/i.test(log)) return 'warning'

  return undefined
}
