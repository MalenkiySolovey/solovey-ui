import { defineComponent } from 'vue'
import OutboundEditor from '@/features/useOutboundEditor'
import EntityDrawer from './EntityDrawer.vue'
import FormSection from './FormSection.vue'

export default defineComponent({
  extends: OutboundEditor,
  inheritAttrs: false,
  components: { EntityDrawer, FormSection },
})
