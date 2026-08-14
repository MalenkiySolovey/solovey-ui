import { defineComponent } from 'vue'
import { OutTypes, createOutbound } from '@/types/outbounds'
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
import Selector from '@/components/protocols/Selector.vue'
import UrlTest from '@/components/protocols/UrlTest.vue'
import Failover from '@/components/protocols/Failover.vue'
import { convertOutboundLink } from '@/shared/composables/useOutboundConversion'
import AnyTls from '@/components/protocols/AnyTls.vue'
import Data from '@/store/modules/data'
export default defineComponent({
  props: ['visible', 'data', 'id', 'tags'],
  emits: ['close'],
  data() {
    return {
      outbound: createOutbound("direct",{ "tag": "" }),
      title: "add",
      tab: "t1",
      link: "",
      loading: false,
      snapshot: "",
      outTypes: OutTypes,
      NoDial: [OutTypes.Selector, OutTypes.URLTest, OutTypes.Failover],
      NoServer: [OutTypes.Direct, OutTypes.Selector, OutTypes.URLTest, OutTypes.Failover, OutTypes.Tor],
    }
  },
  methods: {
    updateData(id: number) {
      if (id > 0) {
        const newData = JSON.parse(this.$props.data)
        this.outbound = createOutbound(newData.type, newData)
        this.title = "edit"
      }
      else {
        this.outbound = createOutbound("direct",{ tag: "direct-" + RandomUtil.randomSeq(3) })
        this.title = "add"
      }
      this.tab = "t1"
      this.snapshot = JSON.stringify(this.outbound)
    },
    changeType() {
      // Tag change only in add outbound
      const tag = this.$props.id > 0 ? this.outbound.tag : this.outbound.type + "-" + RandomUtil.randomSeq(3)
      // Use previous data
      const prevConfig = { id: this.outbound.id, tag: tag, listen: this.outbound.listen, listen_port: this.outbound.listen_port }
      this.outbound = createOutbound(this.outbound.type, prevConfig)
    },
    closeModal() {
      this.updateData(0) // reset
      this.$emit('close')
    },
    async saveChanges() {
      // Guard against double-submit (button is also :disabled while loading).
      if (!this.$props.visible || this.loading) return
      // check duplicate tag
      const isDuplicatedTag = Data().checkTag("outbound",this.$props.id, this.outbound.tag)
      if (isDuplicatedTag) return

      // save data
      this.loading = true
      try {
        const success = await Data().save("outbounds", this.$props.id == 0 ? "new" : "edit", this.outbound)
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
  components: { Dial, Multiplex, Transport, OutTLS,
    Direct, Socks, Http, Shadowsocks, Vmess, Trojan,
    Wireguard, Hysteria, Naive, ShadowTls, Vless, Tuic,
    Hysteria2, AnyTls, Tor, Ssh, Selector, UrlTest, Failover }
})
