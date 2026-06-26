<template>
  <v-menu v-model="menu" :close-on-content-click="false" location="start">
    <template #activator="{ props }">
      <v-btn v-bind="props" hide-details variant="tonal">{{ $t('tls.options') }}</v-btn>
    </template>
    <v-card>
      <v-list>
        <template v-if="tlsType === 0">
          <v-list-item><v-switch v-model="optionSNI" color="primary" label="SNI" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionALPN" color="primary" label="ALPN" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionMinV" color="primary" :label="$t('tls.minVer')" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionMaxV" color="primary" :label="$t('tls.maxVer')" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionCS" color="primary" :label="$t('tls.cs')" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionCurve" color="primary" :label="$t('tls.curves')" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionClientAuth" color="primary" :label="$t('tls.clientAuthentication')" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionFP" color="primary" label="UTLS" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionStore" color="primary" :label="$t('tls.store')" hide-details /></v-list-item>
          <v-list-item><v-switch v-model="optionKtls" color="primary" :label="$t('tls.ktls')" hide-details /></v-list-item>
        </template>
        <v-list-item v-else>
          <v-switch v-model="optionTime" color="primary" label="Max Time Difference" hide-details />
        </v-list-item>
      </v-list>
    </v-card>
  </v-menu>
</template>

<script lang="ts" setup>
defineProps<{ tlsType: number }>()

const menu = defineModel<boolean>('menu', { required: true })
const optionSNI = defineModel<boolean>('optionSNI', { required: true })
const optionALPN = defineModel<boolean>('optionALPN', { required: true })
const optionMinV = defineModel<boolean>('optionMinV', { required: true })
const optionMaxV = defineModel<boolean>('optionMaxV', { required: true })
const optionCS = defineModel<boolean>('optionCS', { required: true })
const optionCurve = defineModel<boolean>('optionCurve', { required: true })
const optionClientAuth = defineModel<boolean>('optionClientAuth', { required: true })
const optionFP = defineModel<boolean>('optionFP', { required: true })
const optionStore = defineModel<boolean>('optionStore', { required: true })
const optionKtls = defineModel<boolean>('optionKtls', { required: true })
const optionTime = defineModel<boolean>('optionTime', { required: true })
</script>
