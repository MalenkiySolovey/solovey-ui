import { defineComponent } from 'vue'
import TlsEditor from '@/features/useTlsEditor'
import EntityDrawer from './EntityDrawer.vue'

export default defineComponent({
  extends: TlsEditor,
  inheritAttrs: false,
  components: { EntityDrawer },
})
