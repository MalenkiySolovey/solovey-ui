import { computed } from 'vue'
import { slotEntries } from './registry'
import type { SlotName } from './types'

export const useSlot = (name: SlotName) => computed(() => slotEntries(name))
