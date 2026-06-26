<template>
    <v-card :subtitle="$t('objects.transport')">
    <v-row>
      <v-col cols="12" sm="6" md="4">
        <v-select
          hide-details
          :label="$t('type')"
          :items="transportItems"
          v-model="transportType">
        </v-select>
      </v-col>
    </v-row>
    <Http v-if="Transport.type == trspTypes.HTTP" :transport="Transport" />
    <WebSocket v-if="Transport.type == trspTypes.WebSocket" :transport="Transport" />
    <GRPC v-if="Transport.type == trspTypes.gRPC" :transport="Transport" />
    <HttpUpgrade v-if="Transport.type == trspTypes.HTTPUpgrade" :transport="Transport" />
  </v-card>
</template>

<script lang="ts">
import { TrspTypes, Transport } from '@/types/transport'
import Http from '@/components/transports/Http.vue'
import WebSocket from '@/components/transports/WebSocket.vue'
import GRPC from '@/components/transports/gRPC.vue'
import HttpUpgrade from '@/components/transports/HttpUpgrade.vue'
export default {
  props: ['data'],
  data() {
    return {
      tcpType: 'tcp',
      trspTypes: TrspTypes
    }
  },
  computed: {
    Transport() {
      return <Transport>(this.$props.data.transport ?? {})
    },
    transportItems() {
      return [
        { title: 'TCP', value: this.tcpType },
        ...Object.keys(this.trspTypes).map((key, index) => ({
          title: key,
          value: Object.values(this.trspTypes)[index],
        })),
      ]
    },
    transportType: {
      get() { return this.Transport.type ?? this.tcpType },
      set(newValue: string) { this.$props.data.transport = newValue == this.tcpType ? {} : { type: newValue } }
    }
  },
  components: { Http, WebSocket, GRPC, HttpUpgrade }
}
</script>
