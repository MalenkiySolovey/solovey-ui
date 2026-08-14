import { defineComponent } from 'vue'
import { tls, iTls, defaultInTls, oTls, defaultOutTls } from '@/types/tls'
import AcmeVue from '@/components/tls/Acme.vue'
import EchVue from '@/components/tls/Ech.vue'
import { push } from 'notivue'
import { i18n } from '@/locales'
import RandomUtil from '@/plugins/randomUtil'
import { generateKeypair, parseRealityKeypair, parseTLSKeypair } from '@/shared/composables/useKeypairs'
export default defineComponent({
  props: ['visible', 'data', 'id'],
  emits: ['close', 'save'],
  data() {
    return {
      tls: <tls>{ id: 0, name: '', server: <iTls>{ enabled: true }, client: <oTls>{} },
      title: "add",
      loading: false,
      menu: false,
      tlsType: 0,
      usePath: 0,
      snapshot: "",
      alpn: [
        { title: "H3", value: 'h3' },
        { title: "H2", value: 'h2' },
        { title: "Http/1.1", value: 'http/1.1' },
      ],
      tlsVersions: [ '1.0', '1.1', '1.2', '1.3' ],
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
      curvePreferences: ['P256', 'P384', 'P521', 'X25519', 'X25519MLKEM768'],
      clientAuthTypes: [
        { title: 'No client certificate', value: 'no' },
        { title: 'Request client certificate', value: 'request' },
        { title: 'Require any client certificate', value: 'require-any' },
        { title: 'Verify if given', value: 'verify-if-given' },
        { title: 'Require and verify', value: 'require-and-verify' },
      ],
      storeItems: [
        { title: "Mozilla", value: "mozilla" },
        { title: "Chrome", value: "chrome" },
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
  methods: {
    updateData(id: number) {
      if (id > 0) {
        const newData = <tls>JSON.parse(this.$props.data)
        this.tls = newData
        if (this.tls.server == null) this.tls.server = { enabled: true }
        if (this.tls.client == null) this.tls.client = {}
        this.tlsType = newData.server?.reality == undefined ? 0 : 1
        this.usePath = newData.server?.key == undefined ? 0 : 1
        this.title = "edit"
      }
      else {
        this.tls = <tls>{ id: 0, name: '', server: {enabled: true}, client: {} }
        this.tlsType = 0
        this.usePath = 0
        this.title = "add"
      }
      this.snapshot = JSON.stringify(this.tls)
    },
    changeTlsType(){
      if (this.tlsType) {
        this.tls.server = <iTls>{
          enabled: true,
          reality: { enabled: true, handshake: { server_port: 443 }, short_id: RandomUtil.randomShortId() },
          server_name: ""
        }
        this.tls.client = <oTls>{ reality: { public_key: "" }, utls: JSON.parse(JSON.stringify(defaultOutTls.utls)) }
      } else {
        this.tls.server = <iTls>{ enabled: true }
        this.tls.client = <oTls>{}
      }
    },
    closeModal() {
      this.updateData(0) // reset
      this.$emit('close')
    },
    saveChanges() {
      this.loading = true
      this.$emit('save', this.tls)
      this.loading = false
    },
    async genSelfSigned(){
      this.loading = true
      const msg = await generateKeypair('tls', this.inTls.server_name ?? "''")
      this.loading = false
      if (msg.success) {
        this.inTls.key_path=undefined
        this.inTls.certificate_path=undefined
        this.usePath = 1
        if (msg.obj.length>0){
          const pair = parseTLSKeypair(msg.obj as string[])
          this.inTls.key = pair.privateKey
          this.inTls.certificate = pair.certificate

        } else {
          push.error({
            message: i18n.global.t('error') + ": " + msg.obj
          })
        }
      }
    },
    async genRealityKey(){
      this.loading = true
      const msg = await generateKeypair('reality')
      this.loading = false
      if (msg.success) {
        const pair = parseRealityKeypair(msg.obj as string[])
        if (this.inTls.reality && this.outTls.reality) {
          if (pair.privateKey) this.inTls.reality.private_key = pair.privateKey
          if (pair.publicKey) this.outTls.reality.public_key = pair.publicKey
        }
      } else {
        push.error({
          message: i18n.global.t('error') + ": " + msg.obj
        })
      }
    },
    randomSID(){
      this.short_id = RandomUtil.randomShortId().join(',')
    }
  },
  computed: {
    dirty(): boolean {
      return this.snapshot !== "" && JSON.stringify(this.tls) !== this.snapshot
    },
    inTls(): iTls {
      return this.tls.server
    },
    outTls(): oTls {
      return this.tls.client
    },
    certText: {
      get(): string { return this.inTls.certificate ? this.inTls.certificate.join('\n') : '' },
      set(v:string) { this.inTls.certificate = v.split('\n') }
    },
    keyText: {
      get(): string { return this.inTls.key ? this.inTls.key.join('\n') : '' },
      set(v:string) { this.inTls.key = v.split('\n') }
    },
    disableSni: {
      get() { return this.outTls.disable_sni ?? false },
      set(v: boolean) { this.tls.client.disable_sni = v ? true : undefined }
    },
    insecure: {
      get() { return this.outTls.insecure ?? false },
      set(v: boolean) { this.tls.client.insecure = v ? true : undefined }
    },
    server_port: {
      get() { return this.inTls.reality?.handshake?.server_port ? this.inTls.reality.handshake.server_port : 443 },
      set(v: any) {
        if (this.inTls.reality){
          this.inTls.reality.handshake.server_port = v.length == 0 || v == 0 ? 443 : parseInt(v)
        }
      }
    },
    short_id: {
      get() { return this.inTls.reality?.short_id ? this.inTls.reality.short_id.join(',') : undefined },
      set(v: string) {
        if (this.inTls.reality){
          this.inTls.reality.short_id = v.length > 0 ? v.split(',') : []
        }
      }
    },
    max_time: {
      get() { return this.inTls?.reality?.max_time_difference ? this.inTls.reality.max_time_difference.replace('m','') : 1 },
      set(v: number) {
        if (this.inTls.reality){
          this.inTls.reality.max_time_difference = v > 0 ? v + 'm' : '1m'
        }
      }
    },
    optionSNI: {
      get(): boolean { return this.inTls.server_name != undefined },
      set(v:boolean) { this.inTls.server_name = v ? '' : undefined }
    },
    optionALPN: {
      get(): boolean { return this.inTls.alpn != undefined },
      set(v:boolean) { this.inTls.alpn = v ? JSON.parse(JSON.stringify(defaultInTls.alpn)) : undefined }
    },
    optionMinV: {
      get(): boolean { return this.inTls.min_version != undefined },
      set(v:boolean) { this.inTls.min_version = v ? defaultInTls.min_version : undefined }
    },
    optionMaxV: {
      get(): boolean { return this.inTls.max_version != undefined },
      set(v:boolean) { this.inTls.max_version = v ? defaultInTls.max_version : undefined }
    },
    optionCS: {
      get(): boolean { return this.inTls.cipher_suites != undefined },
      set(v:boolean) { this.inTls.cipher_suites = v ? JSON.parse(JSON.stringify(defaultInTls.cipher_suites)) : undefined }
    },
    optionCurve: {
      get(): boolean { return this.inTls.curve_preferences != undefined },
      set(v:boolean) { this.inTls.curve_preferences = v ? [] : undefined }
    },
    optionClientAuth: {
      get(): boolean {
        return this.inTls.client_authentication != undefined ||
               this.inTls.client_certificate != undefined ||
               this.inTls.client_certificate_path != undefined ||
               this.inTls.client_certificate_public_key_sha256 != undefined
      },
      set(v:boolean) {
        if (v) {
          this.inTls.client_authentication = 'no'
          this.inTls.client_certificate = []
          this.inTls.client_certificate_path = []
          this.inTls.client_certificate_public_key_sha256 = []
        } else {
          delete this.inTls.client_authentication
          delete this.inTls.client_certificate
          delete this.inTls.client_certificate_path
          delete this.inTls.client_certificate_public_key_sha256
        }
      }
    },
    optionFP: {
      get(): boolean { return this.outTls.utls != undefined },
      set(v:boolean) { this.outTls.utls = v ? JSON.parse(JSON.stringify(defaultOutTls.utls)) : undefined }
    },
    optionStore: {
      get(): boolean { return this.inTls.store != undefined },
      set(v:boolean) { this.inTls.store = v ? 'mozilla' : undefined }
    },
    optionKtls: {
      get(): boolean { return this.inTls.kernel_tx != undefined || this.inTls.kernel_rx != undefined },
      set(v:boolean) {
        if (v) {
          this.inTls.kernel_tx = false
          this.inTls.kernel_rx = false
        } else {
          delete this.inTls.kernel_tx
          delete this.inTls.kernel_rx
        }
      }
    },
    optionEch: {
      get(): boolean { return this.outTls.ech != undefined },
      set(v:boolean) { this.outTls.ech = v ? JSON.parse(JSON.stringify(defaultOutTls.ech)) : undefined }
    },
    optionTime: {
      get(): boolean { return this.inTls?.reality?.max_time_difference != undefined },
      set(v:boolean) { if (this.inTls.reality) this.inTls.reality.max_time_difference = v ? "1m" : undefined }
    },
    clientCertificateText: {
      get(): string { return this.inTls.client_certificate ? this.inTls.client_certificate.join('\n') : '' },
      set(v:string) { this.inTls.client_certificate = v.length > 0 ? v.split('\n') : [] }
    },
    clientCertificatePath: {
      get(): string { return this.inTls.client_certificate_path?.join('\n') ?? '' },
      set(v:string) { this.inTls.client_certificate_path = v.split(/[\n,]/).map((s:string) => s.trim()).filter((s:string) => s.length > 0) }
    },
    clientCertificatePublicKeySha256: {
      get(): string { return this.inTls.client_certificate_public_key_sha256?.join('\n') ?? '' },
      set(v:string) { this.inTls.client_certificate_public_key_sha256 = v.split(/[\n,]/).map((s:string) => s.trim()).filter((s:string) => s.length > 0) }
    }
  },
  watch: {
    visible(v) {
      if (v) {
        this.updateData(this.$props.id)
      }
    },
  },
  components: { AcmeVue, EchVue }
})
