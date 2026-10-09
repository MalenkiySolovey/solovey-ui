import { computed } from 'vue'
import { coreConfigContract } from '@/types/coreConfigContract'

export function useSnellEditor(data: Record<string, any>, direction: 'in' | 'out' | 'out_json', serverVersion?: number) {
  const contract = computed(() => coreConfigContract.value?.protocol?.snell?.[direction === 'in' ? 'in' : 'out'])
  const versionFact = computed(() => {
    const version = direction === 'out_json'
      ? coreConfigContract.value?.protocol?.snell?.in.versions.find(item => item.version === serverVersion)?.clientVersion
      : data.version
    return contract.value?.versions.find(item => item.version === version)
  })
  const pskError = computed(() => {
    const fact = versionFact.value
    if (!fact) return undefined
    const bytes = new TextEncoder().encode(data.psk ?? '').length
    return bytes < fact.pskMinBytes || (fact.pskMaxBytes > 0 && bytes > fact.pskMaxBytes)
      ? { min: fact.pskMinBytes, max: fact.pskMaxBytes || '∞', bytes } : undefined
  })
  function changeVersion(version: number) {
    const fact = contract.value?.versions.find(item => item.version === version)
    if (!fact) return
    data.version = version
    if (fact.obfuscation) delete data.mode
    else { delete data.obfs_mode; delete data.obfs_host }
  }
  return { contract, versionFact, pskError, changeVersion }
}
