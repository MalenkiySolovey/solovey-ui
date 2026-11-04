<template>
  <RuleOptions :rule="rule" :clients="clients" :in-tags="inTags" :rule-sets="ruleSets" />
  <DNSMatchConditions :rule="rule" />
  <v-row v-if="coreConfigContract?.dnsConditions.includes('match_response')" class="px-3">
    <v-col cols="12" sm="4">
      <v-select v-model="responseMode" :items="responseModes" :label="$t('dns.rule.responseContext')" hide-details />
    </v-col>
    <v-col v-if="responseMode === 'tagged'" cols="12" sm="4">
      <v-text-field v-model="rule.match_response" :label="$t('dns.rule.earlierEvaluateTag')" hide-details />
    </v-col>
    <v-col cols="12" sm="4">
      <v-text-field v-model="rule.response_rcode" :label="$t('dns.rule.action.rcode')" clearable @click:clear="delete rule.response_rcode" hide-details />
    </v-col>
    <v-col v-for="field in responseLists" :key="field" cols="12" sm="4">
      <v-textarea :model-value="listText(field)" @update:model-value="setList(field, $event)" :label="$t('dns.rule.' + field)" rows="2" auto-grow hide-details />
    </v-col>
  </v-row>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import RuleOptions from '@/components/rules/DnsRule.vue'
import DNSMatchConditions from './DNSMatchConditions.vue'
import { coreConfigContract } from '@/types/coreConfigContract'
const props = defineProps<{ rule: Record<string, any>; clients?: any; inTags?: any; ruleSets?: any }>()
const { t } = useI18n()
const responseModes = computed(() => ['absent', 'disabled', 'anonymous', 'tagged'].map(value => ({ value, title: t('dns.rule.responseModes.' + value) })))
const responseMode = computed({
  get: () => typeof props.rule.match_response === 'string' ? 'tagged' : props.rule.match_response === true ? 'anonymous' : props.rule.match_response === false ? 'disabled' : 'absent',
  set: (value: string) => { if (value === 'absent') delete props.rule.match_response; else props.rule.match_response = value === 'tagged' ? '' : value === 'anonymous' },
})
const responseLists = ['response_answer', 'response_ns', 'response_extra']
const listText = (field: string) => Array.isArray(props.rule[field]) ? props.rule[field].join('\n') : props.rule[field] ?? ''
const setList = (field: string, value: string) => { if (value === '') delete props.rule[field]; else props.rule[field] = value.split('\n') }
</script>
