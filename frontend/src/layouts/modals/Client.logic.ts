import { defineAsyncComponent, defineComponent } from 'vue'
import { createClient, randomConfigs, updateConfigs, Link, isCoreClientLinkType, shuffleConfigs } from '@/types/clients'
import { HumanReadable } from '@/plugins/utils'
import Data from '@/store/modules/data'
import { dateLocale } from '@/locales'
import FormShell from '@/components/nexus/drawers/FormShell.vue'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
import ComponentSlot from '@/componentSystem/ComponentSlot.vue'

const DatePick = defineAsyncComponent(() => import('@/components/fields/DateTime.vue'))

export default defineComponent({
  props: ['visible', 'id', 'inboundTags', 'groups'],
  emits: ['close'],
  data() {
    return {
      client: createClient(),
      title: "add",
      loading: false,
      tab: "t1",
      clientConfig: <any>[],
      links: <Link[]>[],
      extLinks: <Link[]>[],
      subLinks: <Link[]>[],
      componentLinks: <Link[]>[],
      ipLimitModes: ['monitor', 'enforce'],
      snapshot: '',
    }
  },
  methods: {
    async updateData(id: number) {
      this.loading = true
      if (id > 0) {
        const newData = await Data().loadClients(id)
        this.client = createClient(newData)
        this.title = "edit"
        this.clientConfig = this.client.config
      }
      else {
        this.client = createClient()
        this.title = "add"
        this.clientConfig = randomConfigs('client')
      }
      const clientLinks = this.client.links ?? []
      this.links = clientLinks.filter(l => l.type === 'local')
      this.extLinks = clientLinks.filter(l => l.type === 'external')
      this.subLinks = clientLinks.filter(l => l.type === 'sub')
      this.componentLinks = clientLinks.filter(l => !isCoreClientLinkType(l.type))
      this.tab = "t1"
      this.loading = false
      this.snapshot = JSON.stringify([this.client, this.clientConfig, this.links, this.extLinks, this.subLinks, this.componentLinks])
    },
    closeModal() {
      this.closeSelectMenus()
      this.updateData(0) // reset
      this.$emit('close')
    },
    async saveChanges() {
      // Guard against double-submit: ignore re-entry while a save is in flight
      // (the button is also :disabled while loading).
      if (!this.$props.visible || this.loading) return
      // check duplicate name
      const isDuplicateName = Data().checkClientName(this.$props.id, this.client.name)
      if (isDuplicateName) return

      // check if delayStart is true and autoReset is false, set expiry to 0
      if (this.client.delayStart && !this.client.autoReset) this.client.expiry = 0

      // save data
      this.loading = true
      try {
        this.client.config = updateConfigs(this.clientConfig, this.client.name)
        this.client.links = [
                          ...this.extLinks.filter(l => l.uri != ''),
                          ...this.subLinks.filter(l => l.uri != ''),
                          ...this.componentLinks.filter(l => this.isComponentLink(l))]
        const success = await Data().save("clients", this.$props.id == 0 ? "new" : "edit", this.client)
        if (success) this.closeModal()
      } finally {
        this.loading = false
      }
    },
    setDate(newDate:number){
      this.client.expiry = newDate
    },
    setAllInbounds(){
      this.client.inbounds = this.inboundTags.map((i:any) => typeof i === 'object' ? i.value : i).filter(Boolean).sort()
    },
    closeSelectMenus() {
      window.dispatchEvent(new Event('sui-close-select-menus'))
    },
    shuffle(k?:string) {
      shuffleConfigs(this.clientConfig, k)
    },
    resetUsage(){
      this.client.totalUp = (this.client.totalUp ?? 0) + this.client.up
      this.client.totalDown = (this.client.totalDown ?? 0) + this.client.down
      this.client.up = 0
      this.client.down = 0
    },
    isComponentLink(link: Link) {
      return Boolean(link?.type) && !isCoreClientLinkType(link.type)
    },
  },
  computed: {
    dirty(): boolean {
      return this.snapshot !== '' &&
        JSON.stringify([this.client, this.clientConfig, this.links, this.extLinks, this.subLinks, this.componentLinks]) !== this.snapshot
    },
    slotContext(): Record<string, unknown> {
      return { editor: this }
    },
    clientInbounds: {
      get() { return this.client.inbounds.length>0 ? this.client.inbounds.sort() : [] },
      set(v:number[]) { this.client.inbounds = v.length == 0 ?  [] : v.sort() }
    },
    expDate: {
      get() { return this.client.expiry},
      set(v:any) { this.client.expiry = v }
    },
    Volume: {
      get() { return this.client.volume == 0 ? 0 : (this.client.volume / (1024 ** 3)) },
      set(v:number) { this.client.volume = v > 0 ? v*(1024 ** 3) : 0 }
    },
    delayStart: {
      get() { return this.client.delayStart?? false },
      set(v:boolean) {
        this.client.delayStart = v
        this.client.resetDays = v ? 1 : 0
        if (v && !this.autoReset) this.client.expiry = 0
      }
    },
    autoReset: {
      get() { return this.client.autoReset?? false },
      set(v:boolean) {
        this.client.autoReset = v
        this.client.resetDays = v ? 1 : 0
        if (!v) this.client.nextReset = 0
      }
    },
    resetDays: {
      get() { return this.client.resetDays?? 1 },
      set(v:number|null) {
        if (!v) v = 1
        if (this.client.nextReset && this.client.nextReset > 0) {
          this.client.nextReset += (v-(this.client.resetDays?? 0))*24*60*60
        }
        this.client.resetDays = v
      }
    },
    up() :string { return HumanReadable.sizeFormat(this.client.up) },
    down() :string { return HumanReadable.sizeFormat(this.client.down) },
    total() :string { return HumanReadable.sizeFormat(this.client.down + this.client.up) },
    totalUp() :string { return HumanReadable.sizeFormat((this.client.totalUp ?? 0) + this.client.up) },
    totalDown() :string { return HumanReadable.sizeFormat((this.client.totalDown ?? 0) + this.client.down) },
    nextResetFormatted() :string {
      const ts = this.client.nextReset?? 0
      if (ts == 0) return '-'
      const date = new Date(ts*1000)
      return date.toLocaleString(dateLocale())
    },
    percent() :number { return this.client.volume>0 ? Math.round((this.client.up + this.client.down) *100 / this.client.volume) : 0 },
    percentColor() :string { return (this.client.up+this.client.down) >= this.client.volume ? 'error' : this.percent>90 ? 'warning' : 'success' },
  },
  watch: {
    visible(newValue) {
      if (newValue) {
        this.updateData(this.$props.id)
      } else {
        this.closeSelectMenus()
      }
    },
    tab() {
      this.closeSelectMenus()
    },
  },
  components: { FormShell, DatePick, StrictSelect, ComponentSlot },
})
