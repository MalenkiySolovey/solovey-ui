import { defineComponent } from 'vue'
import { oTls, defaultOutTls } from '@/types/tls'
export default defineComponent({
  props: ['outbound'],
  data() {
    return {
      menu: false,
      usePath: this.$props.outbound?.tls?.certificate? 1:0,
      useEchPath: this.$props.outbound?.tls.ech?.config? 1:0,
      defaults: defaultOutTls,
      alpn: [
        { title: "H3", value: 'h3' },
        { title: "H2", value: 'h2' },
        { title: "Http/1.1", value: 'http/1.1' },
      ],
      tlsVersions: [ '1.0', '1.1', '1.2', '1.3' ],
      curvePreferences: ['P256', 'P384', 'P521', 'X25519', 'X25519MLKEM768'],
      cipher_suites: [
        { title: "RSA-AES128-CBC-SHA", value: "TLS_RSA_WITH_AES_128_CBC_SHA" },
        { title: "RSA-AES256-CBC-SHA", value: "TLS_RSA_WITH_AES_256_CBC_SHA" },
        { title: "RSA-AES128-GCM-SHA256", value: "TLS_RSA_WITH_AES_128_GCM_SHA256" },
        { title: "RSA-AES256-GCM-SHA384", value: "TLS_RSA_WITH_AES_256_GCM_SHA384" },
        { title: "AES128-GCM-SHA256", value: "TLS_AES_128_GCM_SHA256" },
        { title: "AES256-GCM-SHA384", value: "TLS_AES_256_GCM_SHA384" },
        { title: "CHACHA20-POLY1305-SHA256", value: "TLS_CHACHA20_POLY1305_SHA256" },
        { title: "ECDHE-ECDSA-AES128-CBC-SHA", value: "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA" },
        { title: "ECDHE-ECDSA-AES256-CBC-SHA", value: "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA" },
        { title: "ECDHE-RSA-AES128-CBC-SHA", value: "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA" },
        { title: "ECDHE-RSA-AES256-CBC-SHA", value: "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA" },
        { title: "ECDHE-ECDSA-AES128-GCM-SHA256", value: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256" },
        { title: "ECDHE-ECDSA-AES256-GCM-SHA384", value: "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384" },
        { title: "ECDHE-RSA-AES128-GCM-SHA256", value: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256" },
        { title: "ECDHE-RSA-AES256-GCM-SHA384", value: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384" },
        { title: "ECDHE-ECDSA-CHACHA20-POLY1305-SHA256", value: "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256" },
        { title: "ECDHE-RSA-CHACHA20-POLY1305-SHA256", value: "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256" }
      ],
      fingerprints: [
        { title: "Chrome", value: "chrome" },
        { title: "Firefox", value: "firefox" },
        { title: "Microsoft Edge", value: "edge" },
        { title: "Apple Safari", value: "safari" },
        { title: "360", value: "360" },
        { title: "QQ", value: "qq" },
        { title: "Apple IOS", value: "ios" },
        { title: "Android", value: "android" },
        { title: "Random", value: "random" },
        { title: "Randomized", value: "randomized" },
      ]
    }
  },
  computed: {
    tls(): oTls {
      return <oTls> this.$props.outbound.tls
    },
    tlsEnable: {
      get() { return Object.hasOwn(this.tls, 'enabled') ? this.tls.enabled : false },
      set(newValue: boolean) { this.$props.outbound.tls = newValue ? { enabled: true } : { enabled: false } }
    },
    disable_sni: {
      get() { return this.tls.disable_sni ?? false },
      set(newValue: boolean) { this.$props.outbound.tls.disable_sni = newValue ? true : undefined }
    },
    insecure: {
      get() { return this.tls.insecure ?? false },
      set(newValue: boolean) { this.$props.outbound.tls.insecure = newValue ? true : undefined }
    },
    tlsOptional(): boolean {
      return !['hysteria','hysteria2','tuic','shadowtls', 'anytls', 'naive'].includes(this.$props.outbound.type)
    },
    echConfigText: {
      get(): string { return this.tls.ech?.config ? this.tls.ech.config.join('\n') : '' },
      set(newValue:string) { if (this.tls.ech) this.tls.ech.config = newValue.split('\n') }
    },
    optionCert: {
      get(): boolean { return this.tls.certificate != undefined || this.tls.certificate_path != undefined },
      set(v:boolean) {
        this.usePath = 0
        if (v) {
          this.$props.outbound.tls.certificate_path = ""
        } else {
          delete this.$props.outbound.tls.certificate_path
          delete this.$props.outbound.tls.certificate
        }
      }
    },
    optionSNI: {
      get(): boolean { return this.tls.server_name != undefined },
      set(v:boolean) { this.$props.outbound.tls.server_name = v ? '' : undefined }
    },
    optionALPN: {
      get(): boolean { return this.tls.alpn != undefined },
      set(v:boolean) { this.$props.outbound.tls.alpn = v ? defaultOutTls.alpn : undefined }
    },
    optionMinV: {
      get(): boolean { return this.tls.min_version != undefined },
      set(v:boolean) { this.$props.outbound.tls.min_version = v ? defaultOutTls.min_version : undefined }
    },
    optionMaxV: {
      get(): boolean { return this.tls.max_version != undefined },
      set(v:boolean) { this.$props.outbound.tls.max_version = v ? defaultOutTls.max_version : undefined }
    },
    optionCS: {
      get(): boolean { return this.tls.cipher_suites != undefined },
      set(v:boolean) { this.$props.outbound.tls.cipher_suites = v ? defaultOutTls.cipher_suites : undefined }
    },
    optionCurve: {
      get(): boolean { return this.tls.curve_preferences != undefined },
      set(v:boolean) { this.$props.outbound.tls.curve_preferences = v ? [] : undefined }
    },
    optionCertPin: {
      get(): boolean { return this.tls.certificate_public_key_sha256 != undefined },
      set(v:boolean) { this.$props.outbound.tls.certificate_public_key_sha256 = v ? [] : undefined }
    },
    optionClientCert: {
      get(): boolean {
        return this.tls.client_certificate != undefined ||
               this.tls.client_certificate_path != undefined ||
               this.tls.client_key != undefined ||
               this.tls.client_key_path != undefined
      },
      set(v:boolean) {
        if (v) {
          this.$props.outbound.tls.client_certificate = []
          this.$props.outbound.tls.client_certificate_path = ''
          this.$props.outbound.tls.client_key = []
          this.$props.outbound.tls.client_key_path = ''
        } else {
          delete this.$props.outbound.tls.client_certificate
          delete this.$props.outbound.tls.client_certificate_path
          delete this.$props.outbound.tls.client_key
          delete this.$props.outbound.tls.client_key_path
        }
      }
    },
    optionFP: {
      get(): boolean { return this.tls.utls != undefined },
      set(v:boolean) { this.$props.outbound.tls.utls = v ? defaultOutTls.utls : undefined }
    },
    optionReality: {
      get(): boolean { return this.tls.reality != undefined },
      set(v:boolean) { this.$props.outbound.tls.reality = v ? defaultOutTls.reality : undefined }
    },
    optionEch: {
      get(): boolean { return this.tls.ech != undefined },
      set(v:boolean) { this.$props.outbound.tls.ech = v ? defaultOutTls.ech : undefined }
    },
    optionFragment: {
      get(): boolean { return this.tls.fragment != undefined },
      set(v:boolean) {
        if (v) {
          this.$props.outbound.tls.fragment = false
        } else {
          delete this.$props.outbound.tls.fragment
          delete this.$props.outbound.tls.fragment_fallback_delay
          delete this.$props.outbound.tls.record_fragment
        }
      }
    },
    optionKtls: {
      get(): boolean { return this.tls.kernel_tx != undefined || this.tls.kernel_rx != undefined },
      set(v:boolean) {
        if (v) {
          this.$props.outbound.tls.kernel_tx = false
          this.$props.outbound.tls.kernel_rx = false
        } else {
          delete this.$props.outbound.tls.kernel_tx
          delete this.$props.outbound.tls.kernel_rx
        }
      }
    },
    certificatePublicKeySha256: {
      get(): string { return this.tls.certificate_public_key_sha256?.join('\n') ?? '' },
      set(v:string) { this.$props.outbound.tls.certificate_public_key_sha256 = v.split(/[\n,]/).map((s:string) => s.trim()).filter((s:string) => s.length > 0) }
    },
    clientCertificateText: {
      get(): string { return this.tls.client_certificate ? this.tls.client_certificate.join('\n') : '' },
      set(v:string) { this.$props.outbound.tls.client_certificate = v.length > 0 ? v.split('\n') : [] }
    },
    clientKeyText: {
      get(): string { return this.tls.client_key ? this.tls.client_key.join('\n') : '' },
      set(v:string) { this.$props.outbound.tls.client_key = v.length > 0 ? v.split('\n') : [] }
    },
    fragmentFallbackDelay: {
      get(): number { return parseInt(this.tls.fragment_fallback_delay?.replace('ms','')?? '500')?? 500 },
      set(v:number) { this.$props.outbound.tls.fragment_fallback_delay = v>0 ? `${v}ms` : undefined }
    }
  }
})
