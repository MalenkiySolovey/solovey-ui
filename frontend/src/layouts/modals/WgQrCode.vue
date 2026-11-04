<template>
  <v-dialog transition="dialog-bottom-transition" width="400">
    <v-card class="rounded-lg" id="qrcode-modal" :loading="loading">
      <v-card-title>
        <v-row>
          <v-col>{{ $t('types.wg.qrCode') }}</v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto"><v-btn icon="mdi-close" variant="text" :aria-label="$t('actions.close')" @click="$emit('close')" /></v-col>
        </v-row>
      </v-card-title>
      <v-divider></v-divider>
      <v-alert v-if="failed" type="warning" role="alert">{{ $t('types.wg.deliveryUnavailable') }}</v-alert>
      <v-row v-for="link in wgLinks" :key="link.peer">
        <v-col style="text-align: center;">
          <v-chip>{{ $t('types.wg.peer') + ' ' + link.peer }}</v-chip>
          <v-btn icon="mdi-download" variant="text" :aria-label="$t('types.wg.downloadPeer') + ' ' + link.peer" @click="download(link.config, link.peer)" /><br />
          <QrcodeVue v-if="fitsQR(link.config)" :value="link.config" level="L" :size="size" @click="copyToClipboard(link.config)" :margin="1" style="border-radius: .5rem; cursor: copy;" />
          <v-alert v-else type="info">{{ $t('types.wg.qrTooLarge') }}</v-alert>
        </v-col>
      </v-row>
    </v-card>
  </v-dialog>
</template>

<script lang="ts">
import QrcodeVue from 'qrcode.vue'
import Clipboard from 'clipboard'
import { i18n } from '@/locales'
import { push } from 'notivue'
import api from '@/plugins/api'

interface PeerConfig { peer: number; config: string }

export default {
  props: ['data', 'visible'],
  data() {
    return {
      wgLinks: <PeerConfig[]>[],
      loading: false,
      failed: false,
      request: 0,
      controller: undefined as AbortController | undefined,
    }
  },
  methods: {
    async load() {
      this.controller?.abort()
      const request = ++this.request
      const controller = new AbortController()
      this.controller = controller
      this.wgLinks = []
      this.failed = false
      this.loading = true
      try {
        const response = await api.post('api/endpoint-peer-config', {
          id: this.$props.data.id, host: location.hostname.replace(/^\[|\]$/g, ''),
        }, { signal: controller.signal, headers: { 'Content-Type': 'application/json' }, timeout: 15_000 })
        if (request !== this.request || !this.$props.visible) return
        if (response.data.success !== true || !Array.isArray(response.data.obj)) throw new Error('unavailable')
        this.wgLinks = response.data.obj
      } catch {
        if (request === this.request && this.$props.visible) this.failed = true
      } finally {
        if (request === this.request) this.loading = false
      }
    },
    fitsQR(config: string) {
      return new TextEncoder().encode(config).length <= 2953
    },
    copyToClipboard(txt:string) {
      const hiddenButton = document.createElement('button')
      hiddenButton.className = 'clipboard-btn'
      document.body.appendChild(hiddenButton)

      const clipboard = new Clipboard('.clipboard-btn', {
        text: () => txt,
        container: document.getElementById('qrcode-modal')?? undefined
      });

      clipboard.on('success', () => {
        clipboard.destroy()
        push.success({
          message: i18n.global.t('success') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      clipboard.on('error', () => {
        clipboard.destroy()
        push.error({
          message: i18n.global.t('failed') + ": " + i18n.global.t('copyToClipboard'),
          duration: 5000,
        })
      })

      // Perform click on hidden button to trigger copy
      hiddenButton.click()
      document.body.removeChild(hiddenButton)
    },
    download(text: string, i: number) {
      let filename = 'endpoint_' + this.$props.data.id + '_peer_' + i + '.conf';
      let element = document.createElement('a');
      element.setAttribute('href', 'data:text/plain;charset=utf-8,' + encodeURIComponent(text));
      element.setAttribute('download', filename);

      element.style.display = 'none';
      document.body.appendChild(element);

      element.click();
      document.body.removeChild(element);     
    }
  },
  computed: {
    size() {
      if (window.innerWidth > 380) return 300
      if (window.innerWidth > 330) return 280
      return 250
    }
  },
  watch: {
    visible(v) {
      if (v) {
        void this.load()
      } else {
        ++this.request
        this.controller?.abort()
        this.wgLinks = []
        this.loading = false
      }
    },
  },
  beforeUnmount() {
    ++this.request
    this.controller?.abort()
    this.wgLinks = []
  },
  components: { QrcodeVue }
}
</script>
