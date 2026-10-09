<template>
  <v-card>
    <v-card-subtitle v-if="direction !== 'out_json'">Snell</v-card-subtitle>
    <v-alert v-if="!contract" type="warning" variant="tonal">{{ $t('capability.waiting') }}</v-alert>
    <v-row v-if="direction !== 'out_json'">
      <v-col cols="12" sm="4">
        <v-select :model-value="data.version" :items="contract?.versions.map(item => item.version) ?? []" :label="$t('version')" @update:model-value="changeVersion" />
      </v-col>
      <v-col cols="12" sm="8">
        <v-text-field v-model="data.psk" :type="revealPSK ? 'text' : 'password'" :label="$t('types.snell.psk')"
          :append-inner-icon="revealPSK ? 'mdi-eye-off' : 'mdi-eye'" :error-messages="pskError ? [$t('types.snell.pskLength', pskError)] : []"
          @click:append-inner="revealPSK = !revealPSK" />
        <v-btn v-if="direction === 'in'" size="small" @click="data.psk = RandomUtil.randomShadowsocksPassword(24)">{{ $t('reset') }}</v-btn>
      </v-col>
      <v-col v-if="direction === 'out'" cols="12">
        <v-text-field v-model="data.userkey" :type="revealKey ? 'text' : 'password'" :label="$t('types.snell.userKey')"
          :append-inner-icon="revealKey ? 'mdi-eye-off' : 'mdi-eye'" @click:append-inner="revealKey = !revealKey" />
      </v-col>
    </v-row>
    <v-row v-if="versionFact?.obfuscation">
      <v-col v-if="direction !== 'out_json'" cols="12" sm="6">
        <v-select v-model="data.obfs_mode" :items="contract?.obfsModes ?? []" :label="$t('types.snell.obfsMode')" clearable @click:clear="delete data.obfs_mode" />
      </v-col>
      <v-col v-if="direction !== 'in'" cols="12" sm="6">
        <v-text-field v-model="data.obfs_host" :label="$t('types.snell.obfsHost')" clearable @click:clear="delete data.obfs_host" />
      </v-col>
    </v-row>
    <v-select v-if="versionFact && !versionFact.obfuscation && direction !== 'out_json'" v-model="data.mode"
      :items="contract?.modes ?? []" :label="$t('types.snell.mode')" clearable @click:clear="delete data.mode" />
    <v-switch v-if="direction !== 'in'" v-model="data.reuse" :label="$t('types.snell.reuse')" color="primary" />
    <v-alert type="info" variant="tonal" density="compact">{{ $t('types.snell.exportLimit') }}</v-alert>
  </v-card>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { loadCoreConfigContract } from '@/types/coreConfigContract'
import { useSnellEditor } from '@/features/snellEditor'
import RandomUtil from '@/plugins/randomUtil'

const props = defineProps<{ data: Record<string, any>; direction: 'in' | 'out' | 'out_json'; serverVersion?: number }>()
const revealPSK = ref(false)
const revealKey = ref(false)
const { contract, versionFact, pskError, changeVersion } = useSnellEditor(props.data, props.direction, props.serverVersion)
onMounted(() => { void loadCoreConfigContract() })
</script>
