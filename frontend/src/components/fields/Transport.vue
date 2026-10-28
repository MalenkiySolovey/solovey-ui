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
    <v-alert v-if="transportType !== 'tcp' && !facts?.[transportType]?.available" type="warning" variant="tonal" class="my-3">{{ $t('transport.unavailable') }}</v-alert>
    <v-alert v-if="transportType === 'quic' && facts?.quic?.available" type="info" variant="tonal" class="my-3">{{ $t('transport.quicNative') }}</v-alert>
    <Http v-if="Transport.type == trspTypes.HTTP" :transport="Transport" />
    <WebSocket v-if="Transport.type == trspTypes.WebSocket" :transport="Transport" />
    <GRPC v-if="Transport.type == trspTypes.gRPC" :transport="Transport" />
    <HttpUpgrade v-if="Transport.type == trspTypes.HTTPUpgrade" :transport="Transport" />
    <Xhttp v-if="Transport.type === trspTypes.XHTTP && facts?.xhttp?.available" :transport="Transport" :direction="direction" :parent-tag="data.tag" />
    <Mkcp v-if="Transport.type === trspTypes.mKCP && facts?.mkcp?.available" :transport="Transport" />
  </v-card>
</template>

<script lang="ts">
import { TrspTypes, Transport } from '@/types/transport'
import Http from '@/components/transports/Http.vue'
import WebSocket from '@/components/transports/WebSocket.vue'
import GRPC from '@/components/transports/gRPC.vue'
import HttpUpgrade from '@/components/transports/HttpUpgrade.vue'
import Xhttp from '@/components/transports/Xhttp.vue'
import Mkcp from '@/components/transports/Mkcp.vue'
import { coreConfigContract, loadCoreConfigContract } from '@/types/coreConfigContract'
export default {
  props: ['data', 'direction'],
  data() {
    return {
      tcpType: 'tcp',
      trspTypes: TrspTypes
    }
  },
  computed: {
    facts: () => coreConfigContract.value?.protocol?.transports,
    Transport() {
      return <Transport>(this.$props.data.transport ?? {})
    },
    transportItems() {
      return [
        { title: 'TCP', value: this.tcpType },
        ...Object.keys(this.trspTypes).map((key, index) => ({
          title: key,
          value: Object.values(this.trspTypes)[index],
          props: { disabled: !this.facts?.[Object.values(this.trspTypes)[index]]?.available },
        })),
      ]
    },
    transportType: {
      get() { return this.Transport.type ?? this.tcpType },
      set(newValue: string) {
        if (newValue === this.tcpType) this.$props.data.transport = {}
        else if (this.facts?.[newValue]?.available) this.$props.data.transport = { ...this.facts[newValue].defaults, type: newValue }
      }
    }
  },
  mounted() { void loadCoreConfigContract() },
  components: { Http, WebSocket, GRPC, HttpUpgrade, Xhttp, Mkcp }
}
</script>
