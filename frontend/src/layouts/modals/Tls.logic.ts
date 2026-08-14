import { defineComponent } from 'vue'
import TlsOptionsMenu from '@/components/tls/TlsOptionsMenu.vue'
import TlsEditor from '@/features/useTlsEditor'

export default defineComponent({
  extends: TlsEditor,
  components: { TlsOptionsMenu },
})
