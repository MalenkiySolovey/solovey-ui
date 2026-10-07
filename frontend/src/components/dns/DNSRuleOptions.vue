<template>
  <RuleOptions :rule="rule" :clients="clients" :in-tags="inTags" :rule-sets="ruleSets" />
  <v-row v-if="coreConfigContract?.dnsConditions.includes('match_response')" class="px-3">
    <v-col cols="12" sm="4">
      <v-select v-model="responseMode" :items="['absent', 'disabled', 'anonymous', 'tagged']" label="Response context" hide-details />
    </v-col>
    <v-col v-if="responseMode === 'tagged'" cols="12" sm="4">
      <v-text-field v-model="rule.match_response" label="Earlier evaluate tag" hide-details />
    </v-col>
    <v-col cols="12" sm="4">
      <v-text-field v-model="rule.response_rcode" label="Response code" clearable @click:clear="delete rule.response_rcode" hide-details />
    </v-col>
    <v-col v-for="field in responseLists" :key="field" cols="12" sm="4">
      <v-textarea :model-value="listText(field)" @update:model-value="setList(field, $event)" :label="field.replace('response_', 'Response ')" rows="2" auto-grow hide-details />
    </v-col>
  </v-row>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import RuleOptions from '@/components/rules/DnsRule.vue'
import { coreConfigContract } from '@/types/coreConfigContract'
const props = defineProps<{ rule: Record<string, any>; clients?: any; inTags?: any; ruleSets?: any }>()
const responseMode = computed({
  get: () => typeof props.rule.match_response === 'string' ? 'tagged' : props.rule.match_response === true ? 'anonymous' : props.rule.match_response === false ? 'disabled' : 'absent',
  set: (value: string) => { if (value === 'absent') delete props.rule.match_response; else props.rule.match_response = value === 'tagged' ? '' : value === 'anonymous' },
})
const responseLists = ['response_answer', 'response_ns', 'response_extra']
const listText = (field: string) => Array.isArray(props.rule[field]) ? props.rule[field].join('\n') : props.rule[field] ?? ''
const setList = (field: string, value: string) => { if (value === '') delete props.rule[field]; else props.rule[field] = value.split('\n') }
</script>
