import { defineComponent } from 'vue'
import { InTypes, createInbound, Addr, ShadowTLS } from '@/types/inbounds'
import RandomUtil from '@/plugins/randomUtil'
import Dial from '@/components/fields/Dial.vue'
import DomainResolver from '@/components/fields/DomainResolver.vue'
import Listen from '@/components/fields/Listen.vue'
import Direct from '@/components/protocols/Direct.vue'
import Users from '@/components/fields/Users.vue'
import Shadowsocks from '@/components/protocols/Shadowsocks.vue'
import Hysteria from '@/components/protocols/Hysteria.vue'
import Hysteria2 from '@/components/protocols/Hysteria2.vue'
import Naive from '@/components/protocols/Naive.vue'
import ShadowTls from '@/components/protocols/ShadowTls.vue'
import Tuic from '@/components/protocols/Tuic.vue'
import Tun from '@/components/protocols/Tun.vue'
import Trojan from '@/components/protocols/Trojan.vue'
import AnyTls from '@/components/protocols/AnyTls.vue'
import InTls from '@/components/tls/InTLS.vue'
import TProxy from '@/components/protocols/TProxy.vue'
import Multiplex from '@/components/fields/Multiplex.vue'
import Transport from '@/components/fields/Transport.vue'
import AddrVue from '@/components/fields/Addr.vue'
import OutJsonVue from '@/components/subscription/OutJson.vue'
import Data from '@/store/modules/data'
export default defineComponent({
  props: ['visible', 'id', 'inTags', 'tlsConfigs'],
  emits: ['close'],
  data() {
    return {
      inbound: createInbound("direct",{ id:0, "tag": "" }),
      title: "add",
      loading: false,
      snapshot: "",
      side: "s",
      inTypes: InTypes,
      inboundWithUsers: ['mixed', 'socks', 'http', 'shadowsocks', 'vmess', 'trojan', 'naive', 'hysteria', 'shadowtls', 'tuic', 'hysteria2', 'vless', 'anytls'],
      initUsers: {
        model: 'none',
        values: <any>[],
      },
      HasInData: [
        InTypes.SOCKS,
        InTypes.HTTP,
        InTypes.Mixed,
        InTypes.Shadowsocks,
        InTypes.VMess,
        InTypes.ShadowTLS,
        InTypes.Trojan,
        InTypes.Hysteria,
        InTypes.VLESS,
        InTypes.AnyTls,
        InTypes.TUIC,
        InTypes.Hysteria2,
        InTypes.Naive,
      ],
      HasTls: [
        InTypes.HTTP,
        InTypes.VMess,
        InTypes.Trojan,
        InTypes.Naive,
        InTypes.Hysteria,
        InTypes.TUIC,
        InTypes.Hysteria2,
        InTypes.VLESS,
        InTypes.AnyTls,
      ],
      MuxAvailable: [
        InTypes.VLESS,
        InTypes.VMess,
        InTypes.Trojan,
        InTypes.Shadowsocks,
      ],
      OnlyTLS: [InTypes.Hysteria, InTypes.Hysteria2, InTypes.TUIC, InTypes.Naive, InTypes.AnyTls ],
    }
  },
  methods: {
    async loadData(id: number) {
      this.loading = true
      const inboundArray = await Data().loadInbounds([id])
      this.inbound = inboundArray[0]
      if (this.HasInData.includes(this.inbound.type) && this.inbound.out_json == null) {
        this.inbound.out_json = {}
      }
      this.loading = false
      this.snapshot = JSON.stringify(this.inbound)
    },
    updateData(id: number) {
      if (id > 0) {
        this.loadData(id)
        this.title = "edit"
      }
      else {
        const port = RandomUtil.randomIntRange(10000, 60000)
        this.inbound = createInbound("direct",{ id: 0, tag: "direct-"+port ,listen: "::", listen_port: port })
        if (this.HasInData.includes(this.inbound.type)){
          this.inbound.addrs = []
          this.inbound.out_json = {}
        } else {
          delete this.inbound.addrs
          delete this.inbound.out_json
        }
        this.title = "add"
        this.loading = false
        this.snapshot = JSON.stringify(this.inbound)
      }
      this.side = "s"
      this.initUsers = {
        model: 'none',
        values: [],
      }
    },
    changeType() {
      if (!this.inbound.listen_port) this.inbound.listen_port = RandomUtil.randomIntRange(10000, 60000)
      // Tag change only in add inbound
      const tag = this.$props.id > 0 ? this.inbound.tag : this.inbound.type + "-" + this.inbound.listen_port
      // Use previous data
      const prevConfig = { id: this.inbound.id, tag: tag, listen: this.inbound.listen?? "::", listen_port: this.inbound.listen_port }
      this.inbound = createInbound(this.inbound.type, this.inbound.type != this.inTypes.Tun ? prevConfig : { tag: tag })
      if (this.HasInData.includes(this.inbound.type)){
        this.inbound.addrs = []
        this.inbound.out_json = {}
      } else {
        delete this.inbound.addrs
        delete this.inbound.out_json
      }
      this.side = "s"
    },
    add_addr() {
      this.inbound.addrs?.push(<Addr>{ server: location.hostname, server_port: this.inbound.listen_port })
    },
    closeModal() {
      this.updateData(0) // reset
      this.$emit('close')
    },
    async saveChanges() {
      // Guard against double-submit (button is also :disabled while loading).
      if (!this.$props.visible || this.loading) return
      // check duplicate tag
      const isDuplicatedTag = Data().checkTag("inbound", this.inbound.id, this.inbound.tag)
      if (isDuplicatedTag) return

      // save data
      this.loading = true
      try {
        let clientIds = []
        if (this.hasUser) {
          switch (this.initUsers.model) {
            case 'all':
              clientIds = this.clients.map((c:any) => c.id)
              break
            case 'group':
              clientIds = this.clients.filter((c:any) => this.initUsers.values.includes(c.group)).map((c:any) => c.id)
              break
            case 'client':
              clientIds = this.initUsers.values
          }
        }
        const success = await Data().save("inbounds", this.$props.id == 0 ? "new" : "edit", this.inbound, clientIds)
        if (success) this.closeModal()
      } finally {
        this.loading = false
      }
    },
  },
  computed: {
    dirty(): boolean {
      return this.snapshot !== "" && JSON.stringify(this.inbound) !== this.snapshot
    },
    validate() {
      if (this.inbound == undefined) return false
      if (this.inbound.tag == "") return false
      if (this.inbound.listen_port > 65535 || this.inbound.listen_port < 1) return false
      if (this.OnlyTLS.includes(this.inbound.type) && this.inbound.tls_id == 0) return false
      return true
    },
    clients() {
      return Data().clients?? []
    },
    hasUser() {
      if (this.$props.id > 0) return false
      if (!this.inboundWithUsers.includes(this.inbound.type)) return false
      if (this.inbound.type == InTypes.ShadowTLS && (<ShadowTLS>this.inbound).version < 3 ) return false
      if ((<any>this.inbound).managed) return false
      return true
    },
    setSystemProxy: {
      get(): boolean {
        return (<any>this.inbound).set_system_proxy === true
      },
      set(v:boolean) {
        if (v) {
          (<any>this.inbound).set_system_proxy = true
        } else {
          delete (<any>this.inbound).set_system_proxy
        }
      }
    },
  },
  watch: {
    visible(newValue) {
      if (newValue) {
        this.loading = true
      }
    },
  },
  components: {
    Listen, InTls, Hysteria2, Naive, Direct, Shadowsocks,
    Users, Hysteria, ShadowTls, TProxy, Multiplex, Tuic, Tun,
    Trojan, AnyTls, Transport, AddrVue, OutJsonVue, Dial, DomainResolver
  }
})
