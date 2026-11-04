<template>
  <v-card>
    <v-card-text>
      <StrictSelect v-model="data.strategy" :items="[...strategies, 'users']" :label="$t('nativeLimiter.strategy')" @update:model-value="prepareUsers" />
      <p class="text-caption mb-3">{{ $t('nativeLimiter.ownerHint') }}</p>
      <v-card v-for="(row, index) in rows" :key="index" variant="outlined" class="mb-3 pa-3">
        <v-row>
          <template v-if="data.strategy === 'users'">
            <v-col cols="12" sm="6"><StrictSelect v-model="row.name" :items="userNames" :label="$t('nativeLimiter.user')" /></v-col>
            <v-col cols="12" sm="6"><StrictSelect v-model="row.strategy" :items="strategies" :label="$t('nativeLimiter.strategy')" /></v-col>
          </template>
          <template v-if="row.strategy !== 'bypass'">
            <v-col v-if="hasMode" cols="12" sm="6"><StrictSelect v-model="row.mode" :items="['bidirectional', 'download', 'upload']" :label="$t('nativeLimiter.mode')" /></v-col>
            <v-col v-if="data.type !== 'traffic-limiter' && (row.strategy === 'connection' || data.type === 'rate-limiter')" cols="12" sm="6"><StrictSelect v-model="row.connection_type" :items="['default', 'hwid', 'mux', 'source_ip']" :label="$t('nativeLimiter.connectionType')" clearable /></v-col>
            <v-col v-if="hasMode" cols="12" sm="6"><v-text-field v-model="row[quantity]" :label="$t('nativeLimiter.' + quantity)" :placeholder="quantity === 'speed' ? '10MB' : '10GB'" /></v-col>
            <v-col v-else cols="12" sm="6"><v-text-field v-model.number="row.count" type="number" min="0" max="4294967295" :label="$t('nativeLimiter.count')" /></v-col>
            <v-col v-if="data.type === 'rate-limiter'" cols="12" sm="6"><v-text-field v-model="row.interval" :label="$t('nativeLimiter.interval')" placeholder="1s" /></v-col>
          </template>
        </v-row>
        <v-btn v-if="data.strategy === 'users'" variant="text" color="error" @click="data.users.splice(index, 1)">{{ $t('actions.del') }}</v-btn>
      </v-card>
      <v-btn v-if="data.strategy === 'users'" variant="tonal" @click="addUser">{{ $t('actions.add') }}</v-btn>
      <v-combobox v-if="data.type === 'bandwidth-limiter'" v-model="data.flow_keys" :items="['user', 'source_ip', 'hwid', 'mux', 'protocol', 'destination']" multiple chips :label="$t('nativeLimiter.flowKeys')" />
      <StrictSelect v-model="data.route.final" :items="tags" :label="$t('nativeLimiter.final')" />
      <v-textarea :model-value="rulesText" @update:model-value="setRules" :label="$t('nativeLimiter.rules')" :error-messages="rulesError ? [$t('nativeLimiter.rulesInvalid')] : []" rows="4" auto-grow />
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { Outbound } from '@/types/outbounds'
import { createOutbound } from '@/types/outbounds'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
import Data from '@/store/modules/data'

const props = defineProps<{ data: Outbound; tags?: string[] }>()
const emit = defineEmits<{ validity: [valid: boolean] }>()
const rulesText = ref(''), rulesError = ref(false)
const strategies = computed(() => props.data.type === 'rate-limiter'
  ? ['leaky-bucket', 'token-bucket', 'sliding-window', 'fixed-window', 'bypass']
  : props.data.type === 'connection-limiter' ? ['connection', 'bypass']
  : props.data.type === 'traffic-limiter' ? ['global', 'bypass'] : ['global', 'connection', 'bypass'])
const rows = computed(() => props.data.strategy === 'users' ? props.data.users ?? [] : [props.data])
const userNames = computed(() => {
  const choices = new Map<string, { title: string; value: string }>()
  for (const user of Data().clients) {
    for (const [kind, fields] of Object.entries(user.config ?? {})) {
      if (kind.startsWith('_') || !fields || typeof fields !== 'object') continue
      const names = fields as Record<string, unknown>
      const principal = names.name ?? names.username
      if (typeof principal === 'string' && principal.trim() && !choices.has(principal)) {
        choices.set(principal, { title: user.name === principal ? principal : user.name + ' (' + principal + ')', value: principal })
      }
    }
  }
  return [...choices.values()]
})
const hasMode = computed(() => ['bandwidth-limiter', 'traffic-limiter'].includes(props.data.type))
const quantity = computed(() => props.data.type === 'bandwidth-limiter' ? 'speed' : 'total')
function prepareUsers() { if (props.data.strategy === 'users' && !Array.isArray(props.data.users)) props.data.users = [] }
function addUser() {
  prepareUsers()
  const defaults: Partial<Outbound> = createOutbound(props.data.type)
  delete defaults.type; delete defaults.route
  props.data.users.push({ ...defaults, name: '' })
}
function setRules(value: string) {
  rulesText.value = value
  try {
    const rules: unknown = value.trim() ? JSON.parse(value) : []
    if (!Array.isArray(rules)) throw new Error('array required')
    if (rules.length) props.data.route.rules = rules
    else delete props.data.route.rules
    rulesError.value = false
  } catch { rulesError.value = true }
  emit('validity', !rulesError.value)
}
watch(() => props.data, () => {
  if (!props.data.route) props.data.route = {}
  rulesText.value = JSON.stringify(props.data.route.rules ?? [], null, 2)
  rulesError.value = false
  emit('validity', true)
}, { immediate: true })
</script>
