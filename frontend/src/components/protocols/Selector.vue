<template>
  <v-card subtitle="Selector">
    <v-row>
      <v-col cols="12" sm="6">
        <StrictSelect
          v-model="data.outbounds"
          :items="tags"
          :label="$t('pages.outbounds')"
          multiple
          @update:model-value="updateDefault"
          chips
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6" md="4">
        <StrictSelect
          v-model="data.default"
          :items="data.outbounds"
          :label="$t('types.lb.defaultOut')"
          clearable
          hide-details
        />
      </v-col>
      <v-col cols="12" sm="6">
        <v-switch v-model="data.interrupt_exist_connections" color="primary" :label="$t('types.lb.interruptConn')" hide-details></v-switch>
      </v-col>
    </v-row>
  </v-card>
</template>

<script lang="ts">
import StrictSelect from '@/shared/ui/StrictSelect.vue'

export default {
  components: { StrictSelect },
  props: ['data','tags'],
  data() {
    return {}
  },
  methods: {
    updateDefault() {
      if (!this.$props.data.outbounds?.includes(this.$props.data.default)) {
        delete this.$props.data.default
      }
    }
  },
}
</script>
