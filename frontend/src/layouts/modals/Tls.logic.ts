import { defineComponent } from 'vue'
import TlsOptionsMenu from '@/components/tls/TlsOptionsMenu.vue'
import TlsEditor from '@/shared/composables/useTlsEditor'

export default defineComponent({
  extends: TlsEditor,
  components: { TlsOptionsMenu },
})
