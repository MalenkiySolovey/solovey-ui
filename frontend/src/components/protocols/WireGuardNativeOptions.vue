<template>
  <v-expansion-panels multiple class="my-3">
    <v-expansion-panel :title="$t('types.wg.nativeNetwork')">
      <v-expansion-panel-text><v-row>
        <v-col v-for="key in ['udp_mapping', 'udp_filtering']" :key="key" cols="12" sm="6">
          <v-select :model-value="data[key]" @update:model-value="setField(key, $event)" :items="natBehaviors" clearable :label="$t('types.wg.' + key)" />
        </v-col>
        <v-col cols="12" sm="6"><v-text-field :model-value="data.udp_nat_max" @update:model-value="setNumber('udp_nat_max', $event)" type="number" min="0" :label="$t('types.wg.udp_nat_max')" /></v-col>
        <v-col v-if="fieldAvailable('preallocated_buffers_per_pool') || data.preallocated_buffers_per_pool !== undefined" cols="12" sm="6">
          <v-text-field :model-value="data.preallocated_buffers_per_pool" @update:model-value="setNumber('preallocated_buffers_per_pool', $event)" type="number" min="0" :label="$t('types.wg.preallocated_buffers_per_pool')" />
        </v-col>
        <v-col v-if="fieldAvailable('disable_pauses') || data.disable_pauses !== undefined" cols="12" sm="6">
          <v-switch :model-value="data.disable_pauses" @update:model-value="setField('disable_pauses', $event)" :label="$t('types.wg.disable_pauses')" />
        </v-col>
      </v-row></v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel v-if="fieldAvailable('amnezia') || data.amnezia != null" :title="$t('types.amnezia.title')" data-testid="wireguard-amnezia">
      <v-expansion-panel-text>
        <v-switch :model-value="amneziaEnabled" @update:model-value="setEnabled($event === true)" :disabled="!fieldAvailable('amnezia')" :label="$t('types.amnezia.enable')" />
        <template v-if="amneziaEnabled">
          <v-alert type="info" variant="tonal" class="mb-3">{{ $t('types.amnezia.peerHint') }}</v-alert>
          <v-row>
            <v-col v-for="key in ['jc', 'jmin', 'jmax', 's1', 's2', 's3', 's4']" :key="key" cols="12" sm="6" md="4">
              <v-text-field :model-value="data.amnezia[key]" @update:model-value="setAmneziaNumber(key, $event)" type="number" min="0" :label="$t('types.amnezia.' + key)" />
            </v-col>
            <v-col v-for="key in ranges" :key="key" cols="12" sm="6">
              <v-text-field :model-value="data.amnezia[key]" @update:model-value="setAmneziaRange(key, $event)" :label="$t('types.amnezia.' + key)" :hint="$t('types.amnezia.rangeHint')" persistent-hint />
            </v-col>
            <v-col v-for="key in ['i1', 'i2', 'i3', 'i4', 'i5']" :key="key" cols="12">
              <v-textarea :model-value="data.amnezia[key]" @update:model-value="setAmnezia(key, $event || undefined)" rows="2" auto-grow :label="$t('types.amnezia.packetTemplate') + ' (' + key.toUpperCase() + ')'" />
            </v-col>
            <v-col cols="12"><SecretCredentialField :model-value="data.amnezia.header_protection_key" @update:model-value="setAmnezia('header_protection_key', $event)" :available="fieldAvailable('amnezia')" :label="$t('types.amnezia.header_protection_key')" /></v-col>
            <v-col v-for="key in ['random_trailers', 'disable_cookies']" :key="key" cols="12" sm="6">
              <v-switch :model-value="data.amnezia[key]" @update:model-value="setAmnezia(key, $event)" :label="$t('types.amnezia.' + key)" />
            </v-col>
          </v-row>
        </template>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { coreConfigContract, loadCoreConfigContract } from '@/types/coreConfigContract'
import { setWireGuardAmneziaEnabled, wireGuardAmneziaEnabled } from '@/features/endpointCredentialDraft'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'

const props = defineProps<{ data: Record<string, any> }>()
const natBehaviors = ['endpoint_independent', 'address_dependent', 'address_and_port_dependent']
const ranges = ['h1', 'h2', 'h3', 'h4', 'content_padding_addition', 'rekey_after_time', 'rekey_timeout', 'reject_after_time', 'keepalive_timeout', 'max_handshake_attempts']
onMounted(() => { void loadCoreConfigContract() })
const amneziaEnabled = computed(() => wireGuardAmneziaEnabled(props.data))
function fieldAvailable(key: string): boolean {
  return coreConfigContract.value?.protocol?.criticalFields?.[`${props.data.type}/endpoint`]?.[key]?.available === true
}
function setField(key: string, value: unknown) {
  if (['preallocated_buffers_per_pool', 'disable_pauses'].includes(key) && !fieldAvailable(key)) return
  if (value === '' || value === null || value === undefined) delete props.data[key]
  else props.data[key] = value
}
function setNumber(key: string, value: unknown) { setField(key, value === '' || value == null ? undefined : Number(value)) }
function setEnabled(value: boolean) { if (fieldAvailable('amnezia')) setWireGuardAmneziaEnabled(props.data, value) }
function setAmnezia(key: string, value: unknown) {
  if (!fieldAvailable('amnezia') || !amneziaEnabled.value) return
  if (value === '' || value === null || value === undefined) delete props.data.amnezia[key]
  else props.data.amnezia[key] = value
}
function setAmneziaNumber(key: string, value: unknown) { setAmnezia(key, value === '' || value == null ? undefined : Number(value)) }
function setAmneziaRange(key: string, value: unknown) {
  const text = value == null ? '' : String(value).trim()
  setAmnezia(key, /^\d+$/.test(text) ? Number(text) : text || undefined)
}
</script>
