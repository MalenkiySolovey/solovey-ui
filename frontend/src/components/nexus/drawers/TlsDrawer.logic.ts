import { defineComponent } from 'vue'
import TlsEditor from '@/shared/composables/useTlsEditor'
import EntityDrawer from './EntityDrawer.vue'

export default defineComponent({
  extends: TlsEditor,
  inheritAttrs: false,
  components: { EntityDrawer },
})
