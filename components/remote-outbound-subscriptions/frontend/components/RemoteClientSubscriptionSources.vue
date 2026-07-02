<template>
  <v-row v-if="editor">
    <v-col>
      <StrictSelect
        v-model="selected"
        :items="items"
        :label="$t('client.subscriptionTags')"
        clearable
        multiple
        chips
        hide-details
      />
    </v-col>
  </v-row>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
import { loadRemoteOutboundSubscriptions } from '../composables/useRemoteOutboundCatalog'
import { remoteClientSelectionValues, replaceRemoteClientLinks } from '../composables/remotesub/clientLinks'

const props = defineProps<{
  ctx?: {
    editor?: any
  }
}>()

const remoteSubscriptions = ref<any[]>([])
const editor = computed(() => props.ctx?.editor)

const items = computed<{ title: string; value: string }[]>(() => {
  const result: { title: string; value: string }[] = []
  for (const subscription of remoteSubscriptions.value ?? []) {
    const allCount = (subscription.connections ?? []).length
    result.push({
      title: `${subscription.name} / All (${allCount})`,
      value: `subscription:${subscription.id}`,
    })
    for (const group of subscription.groups ?? []) {
      const count = (subscription.connections ?? []).filter((connection: any) => {
        const groupIds = connection.groupIds?.length ? connection.groupIds : (connection.groupId ? [connection.groupId] : [])
        return groupIds.includes(group.id)
      }).length
      result.push({
        title: `${subscription.name} / ${group.name} (${count})`,
        value: `group:${group.id}`,
      })
    }
  }
  return result
})

const selected = computed<string[]>({
  get() {
    return remoteClientSelectionValues(editor.value?.componentLinks ?? [])
  },
  set(ids: string[]) {
    if (!editor.value) return
    const names = new Map(items.value.map(item => [item.value, item.title]))
    editor.value.componentLinks = replaceRemoteClientLinks(editor.value.componentLinks ?? [], ids ?? [], names)
  },
})

onMounted(async () => {
  const msg = await loadRemoteOutboundSubscriptions()
  if (msg.success) {
    remoteSubscriptions.value = msg.obj ?? []
  }
})
</script>
