import { onMounted } from 'vue'
import Data from '@/store/modules/data'
import { coreConfigContract, loadCoreConfigContract } from '@/types/coreConfigContract'
import { capabilityEditState } from '@/types/capabilityEditors'

// Both list layouts and their editors use the same observed capability facts.
export function useResourceAvailability(category: string) {
 onMounted(() => { void loadCoreConfigContract() })
 return {
  resourceAvailability: (row: { type: string; transport?: unknown }) =>
   capabilityEditState(Data().capabilities, category, row.type, row.type, row.transport, coreConfigContract.value?.protocol?.transports, row, coreConfigContract.value?.protocol?.criticalFields),
  canCreate: () => Data().capabilities?.facts.some(fact => fact.category === category && fact.available === true) === true,
 }
}
