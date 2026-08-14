import { defineComponent } from 'vue'
import ServiceEditor from '@/features/useServiceEditor'
import EntityDrawer from './EntityDrawer.vue'
import FormSection from './FormSection.vue'

export default defineComponent({
  extends: ServiceEditor,
  inheritAttrs: false,
  components: { EntityDrawer, FormSection },
})
