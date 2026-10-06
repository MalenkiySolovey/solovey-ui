import { computed } from 'vue'
import { nexusMenuGroups } from '@/componentSystem/navigation'
import type { NavigationCountKey } from '@/navigation/core'
export { nexusMenuGroups } from '@/componentSystem/navigation'
export type { NexusMenuItem, NexusMenuGroup } from '@/componentSystem/navigation'
export type NexusCountKey = NavigationCountKey

export const nexusMenu = computed(() => nexusMenuGroups.value.flatMap(group => group.items))
export const nexusSingBoxSettingsPaths = computed(() => nexusMenu.value.filter(item => item.singBoxSettings).map(item => item.path))
export const visibleNexusMenuGroups = () => nexusMenuGroups.value
