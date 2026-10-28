<template>
  <CapabilityNotice :state="state" />
  <p v-if="state.historical" class="text-subtitle-2 my-2">{{ type }}</p>
  <fieldset :disabled="state.historical" :class="{ 'capability-inspection': state.historical }">
    <legend v-if="state.historical" class="text-caption">{{ $t('capability.inspect') }}</legend>
    <v-defaults-provider :defaults="{ global: { disabled: state.historical === true } }">
      <slot />
    </v-defaults-provider>
  </fieldset>
</template>
<script setup lang="ts">
import { onMounted } from 'vue'
import type { CapabilityEditState } from '@/types/capabilityEditors'
import { loadCoreConfigContract } from '@/types/coreConfigContract'
import CapabilityNotice from './CapabilityNotice.vue'
defineProps<{ state: CapabilityEditState; type: string }>()
onMounted(() => { void loadCoreConfigContract() })
</script>
<style scoped>
fieldset { border: 0; margin: 0; padding: 0; min-width: 0; }
/* Native fieldset semantics also cover controls with an explicit disabled=false.
   Keep the text in the accessibility tree; no inert/aria-hidden boundary. */
.capability-inspection { pointer-events: none; }
</style>
