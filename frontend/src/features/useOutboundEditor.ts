import { defineComponent } from 'vue'
import { coreConfigContract } from '@/types/coreConfigContract'
import { OutTypes, createOutbound, nativeOutboundGroupValid } from '@/types/outbounds'
import RandomUtil from '@/plugins/randomUtil'
import Dial from '@/components/fields/Dial.vue'
import Multiplex from '@/components/fields/Multiplex.vue'
import Transport from '@/components/fields/Transport.vue'
import OutTLS from '@/components/tls/OutTLS.vue'
import Direct from '@/components/protocols/Direct.vue'
import Socks from '@/components/protocols/Socks.vue'
import Http from '@/components/protocols/Http.vue'
import Shadowsocks from '@/components/protocols/Shadowsocks.vue'
import Vmess from '@/components/protocols/Vmess.vue'
import Trojan from '@/components/protocols/Trojan.vue'
import Wireguard from '@/components/protocols/Wireguard.vue'
import Hysteria from '@/components/protocols/Hysteria.vue'
import Naive from '@/components/protocols/Naive.vue'
import ShadowTls from '@/components/protocols/OutShadowTls.vue'
import Vless from '@/components/protocols/Vless.vue'
import Tuic from '@/components/protocols/Tuic.vue'
import Hysteria2 from '@/components/protocols/Hysteria2.vue'
import Tor from '@/components/protocols/Tor.vue'
import Ssh from '@/components/protocols/Ssh.vue'
import CallNative from '@/components/protocols/CallInbound.vue'
import NativeClientOutbound from '@/components/protocols/NativeClientOutbound.vue'
import MasqueOutbound from '@/components/protocols/MasqueOutbound.vue'
import NativeOutboundGroup from '@/components/protocols/NativeOutboundGroup.vue'
import SudokuNative from '@/components/protocols/Sudoku.vue'
import { secretMutation, unchangedSecret } from './secretCredentialDraft'
import { serializeOutboundCredentialDraft } from './outboundCredentialDraft'
import Selector from '@/components/protocols/Selector.vue'
import UrlTest from '@/components/protocols/UrlTest.vue'
import Failover from '@/components/protocols/Failover.vue'
import { convertOutboundLink } from '@/shared/composables/useOutboundConversion'
import AnyTls from '@/components/protocols/AnyTls.vue'
import Snell from '@/components/protocols/Snell.vue'
import Data from '@/store/modules/data'
import CapabilitySurface from '@/components/fields/CapabilitySurface.vue'
import SaveGuardNotice from '@/components/fields/SaveGuardNotice.vue'
import { entitySaveReasons, type SaveReason } from './entitySaveReview'
import { capabilityEditState, capabilityTypeChoices, type CapabilityChoice, type CapabilityEditState } from '@/types/capabilityEditors'
export default defineComponent({
  props: ['visible', 'data', 'id', 'tags'],
  emits: ['close'],
  data() {
    return {
      outbound: createOutbound("direct",{ "tag": "" }),
      title: "add",
      storedCapabilityType: "",
      tab: "t1",
      link: "",
      loading: false,
      snapshot: "",
      outTypes: OutTypes,
      NoDial: [OutTypes.Selector, OutTypes.URLTest, OutTypes.Failover, OutTypes.Bond, OutTypes.CoreFailover],
      NoServer: [OutTypes.Direct, OutTypes.Selector, OutTypes.URLTest, OutTypes.Failover, OutTypes.Tor, OutTypes.Call, OutTypes.MASQUE, OutTypes.Bond, OutTypes.CoreFailover],
    }
  },
  methods: {
    updateData(id: number) {
      if (id > 0) {
        const newData = JSON.parse(this.$props.data)
        this.storedCapabilityType = newData.type
        this.outbound = createOutbound(newData.type, newData)
        this.title = "edit"
      }
      else {
        this.outbound = createOutbound("direct",{ tag: "direct-" + RandomUtil.randomSeq(3) })
        this.title = "add"
        this.storedCapabilityType = ""
      }
      this.tab = "t1"
      this.snapshot = JSON.stringify(this.outbound)
    },
    changeType() {
      // Tag change only in add outbound
      const tag = this.$props.id > 0 ? this.outbound.tag : this.outbound.type + "-" + RandomUtil.randomSeq(3)
      // Use previous data
      const prevConfig = { id: this.outbound.id, tag: tag, listen: this.outbound.listen, listen_port: this.outbound.listen_port }
      this.outbound = createOutbound(this.outbound.type, [OutTypes.Call, OutTypes.Mieru, OutTypes.TrustTunnel, OutTypes.Sudoku, OutTypes.MASQUE, OutTypes.Bond, OutTypes.CoreFailover].includes(this.outbound.type) ? { id: this.outbound.id, tag } : prevConfig)
    },
    closeModal() {
      this.updateData(0) // reset
      this.$emit('close')
    },
    async saveChanges() {
      // Guard against double-submit (button is also :disabled while loading).
      if (!this.$props.visible || this.saveReasons.length > 0) return
      // check duplicate tag
      const isDuplicatedTag = Data().checkTag("outbound",this.$props.id, this.outbound.tag)
      if (isDuplicatedTag) return

      // save data
      this.loading = true
      try {
        const success = await Data().save("outbounds", this.$props.id == 0 ? "new" : "edit", serializeOutboundCredentialDraft(this.outbound))
        if (success) this.closeModal()
      } finally {
        this.loading = false
      }
    },
    async linkConvert() {
      if (this.link.length>0){
        this.loading = true
        const msg = await convertOutboundLink(this.link)
        this.loading = false
        if (msg.success) {
          this.outbound = msg.obj
          if (this.$props.id > 0) this.outbound.id = this.$props.id
          this.tab = "t1"
          this.link = ""
        }
      }
    }
  },
  computed: {
    saveReasons(): SaveReason[] {
      let credentialInvalid = false
      if (this.outbound.type === OutTypes.Call) {
        try {
          const join = secretMutation(this.outbound.join_link)
          const cookies = secretMutation(this.outbound.cookies)
          credentialInvalid = !(join?.intent === 'replace' || unchangedSecret(join)?.present === true) ||
            (cookies?.intent === 'replace' && !cookies.value.trim())
        } catch { credentialInvalid = true }
      }
      if ([OutTypes.Mieru, OutTypes.TrustTunnel].includes(this.outbound.type)) {
        try {
          const password = secretMutation(this.outbound.password)
          credentialInvalid = !this.outbound.username?.trim() || !(password?.intent === 'replace' || unchangedSecret(password)?.present === true)
        } catch { credentialInvalid = true }
      }
      if ([OutTypes.MASQUE, OutTypes.Sudoku].includes(this.outbound.type)) {
        try {
          if (this.outbound.type === OutTypes.MASQUE) {
            const token = secretMutation(this.outbound.profile?.auth_token)
            const present = token?.intent === 'replace' || unchangedSecret(token)?.present === true
            credentialInvalid = Boolean(this.outbound.profile?.id?.trim()) !== present
            for (const field of ['auth_token', 'license_key', 'private_key']) secretMutation(this.outbound.profile?.[field])
          } else {
            const key = secretMutation(this.outbound.key)
            credentialInvalid = !(key?.intent === 'replace' || unchangedSecret(key)?.present === true)
          }
        } catch { credentialInvalid = true }
      }
      const rangeOnly = this.outbound.type === OutTypes.Mieru && !this.outbound.server_port &&
        (typeof this.outbound.server_ports === 'string' ? this.outbound.server_ports.trim().length > 0 : this.outbound.server_ports?.length > 0)
      if (this.outbound.type === OutTypes.Hysteria2 && this.outbound.realm) {
        try { secretMutation(this.outbound.realm.token) } catch { credentialInvalid = true }
        credentialInvalid ||= !this.outbound.realm.server_url?.trim() || !this.outbound.realm.realm_id?.trim()
      }
      const reasons = entitySaveReasons({ identity: this.outbound.tag, port: this.outbound.server_port, requiresPort: this.requiresServer && !rangeOnly, pending: this.loading, capability: this.capabilityState, credentialInvalid, missingRequiredTls: this.outbound.type === OutTypes.TrustTunnel && !this.outbound.tls?.enabled })
      if (!nativeOutboundGroupValid(this.outbound)) reasons.push('nativeOutbound.groupInvalid')
      return reasons
    },
    capabilityState(): CapabilityEditState {
      return capabilityEditState(Data().capabilities, "outbounds", this.outbound.type, this.storedCapabilityType, (<any>this.outbound).transport, coreConfigContract.value?.protocol?.transports, this.outbound, coreConfigContract.value?.protocol?.criticalFields)
    },
    requiresServer(): boolean {
      return !this.NoServer.includes(this.outbound.type) && !(this.outbound.type === OutTypes.Hysteria2 && this.outbound.realm)
    },
    typeChoices(): CapabilityChoice[] {
      return capabilityTypeChoices(Data().capabilities, "outbounds", this.outTypes)
    },
    dirty(): boolean {
      return this.snapshot !== "" && JSON.stringify(this.outbound) !== this.snapshot
    },
  },
  watch: {
    visible(newValue) {
      if (newValue) {
        this.updateData(this.$props.id)
      }
    },
  },
  components: { SaveGuardNotice, CapabilitySurface, Dial, Multiplex, Transport, OutTLS,
    Direct, Socks, Http, Shadowsocks, Vmess, Trojan,
    Wireguard, Hysteria, Naive, ShadowTls, Vless, Tuic,
    Hysteria2, AnyTls, Snell, Tor, Ssh, CallNative, NativeClientOutbound, SudokuNative, MasqueOutbound, NativeOutboundGroup, Selector, UrlTest, Failover }
})
