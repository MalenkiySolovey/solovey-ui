<template>
  <section v-if="present.length || choices.length" class="px-3 py-2" :aria-label="$t('dns.rule.additionalConditions')">
    <v-menu v-if="choices.length" :close-on-content-click="true">
      <template #activator="{ props: menuProps }">
        <v-btn v-bind="menuProps" variant="tonal" prepend-icon="mdi-plus">{{ $t('dns.rule.additionalConditions') }}</v-btn>
      </template>
      <v-list role="menu">
        <v-list-item v-for="field in choices" :key="field.key" role="menuitem" :title="$t(field.label)" @click="add(field)" />
      </v-list>
    </v-menu>
    <v-row v-for="field in present" :key="field.key" class="mt-1" align="center">
      <v-col>
        <v-alert v-if="!available(field.key)" type="info" variant="tonal" density="compact">{{ $t('dns.rule.preservedCondition') }}</v-alert>
        <v-switch v-if="field.kind === 'boolean'" :model-value="rule[field.key]" :disabled="!available(field.key)"
          :label="$t(field.label)" color="primary" hide-details @update:model-value="value => updateBoolean(field.key, value)" />
        <v-textarea v-else :model-value="text(field.key)" :disabled="!available(field.key)" :label="$t(field.label)"
          rows="2" auto-grow hide-details @update:model-value="value => updateList(field.key, value)" />
        <div v-if="field.key === 'preferred_by'" class="text-caption">{{ $t('dns.rule.preferredByHint') }}</div>
      </v-col>
      <v-col v-if="available(field.key)" cols="auto">
        <v-btn icon="mdi-close" size="small" variant="text" :aria-label="$t('dns.rule.removeCondition', { field: $t(field.label) })" @click="delete rule[field.key]" />
      </v-col>
    </v-row>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { availableCreationChoices } from '@/types/capabilityEditors'
import { coreConfigContract, loadCoreConfigContract } from '@/types/coreConfigContract'

const props = defineProps<{ rule: Record<string, any> }>()
type MatchField = { key: string; label: string; kind?: 'boolean' }
const fields: MatchField[] = [
  { key: 'query_client_subnet', label: 'dns.rule.queryClientSubnet' },
  { key: 'query_dnssec', label: 'dns.rule.queryDnssec', kind: 'boolean' },
  { key: 'process_name', label: 'rule.processName' },
  { key: 'process_path', label: 'rule.processPath' },
  { key: 'process_path_regex', label: 'rule.processPathRegex' },
  { key: 'package_name', label: 'rule.packageName' },
  { key: 'package_name_regex', label: 'dns.rule.packageNameRegex' },
  { key: 'source_mac_address', label: 'dns.rule.sourceMacAddress' },
  { key: 'source_hostname', label: 'dns.rule.sourceHostname' },
  { key: 'preferred_by', label: 'dns.rule.preferredBy' },
]
const available = (field: string) => coreConfigContract.value?.dnsConditions.includes(field) === true
const presentKeys = computed(() => new Set(Object.keys(props.rule)))
const present = computed(() => fields.filter(field => presentKeys.value.has(field.key)))
const choices = computed(() => availableCreationChoices(fields, field => available(field.key) && !presentKeys.value.has(field.key)))
function add(field: MatchField) {
  if (available(field.key) && !Object.hasOwn(props.rule, field.key)) props.rule[field.key] = field.kind === 'boolean' ? false : []
}
const text = (field: string) => Array.isArray(props.rule[field]) ? props.rule[field].join('\n') : props.rule[field] ?? ''
function updateList(field: string, value: string) {
  if (available(field)) props.rule[field] = value.split(/\r?\n/).map(item => item.trim()).filter(Boolean)
}
function updateBoolean(field: string, value: boolean | null) {
  if (available(field) && value !== null) props.rule[field] = value
}
onMounted(() => { void loadCoreConfigContract() })
</script>
