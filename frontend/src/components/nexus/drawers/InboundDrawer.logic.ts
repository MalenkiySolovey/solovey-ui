import { defineComponent } from 'vue'
import InboundEditor from '@/shared/composables/useInboundEditor'
import EntityDrawer from './EntityDrawer.vue'
import FormSection from './FormSection.vue'

export default defineComponent({
  extends: InboundEditor,
  inheritAttrs: false,
  watch: {
    visible(visible: boolean) {
      if (visible) {
        this.loading = true
        this.updateData(this.$props.id)
      }
    },
  },
  components: { EntityDrawer, FormSection },
})
