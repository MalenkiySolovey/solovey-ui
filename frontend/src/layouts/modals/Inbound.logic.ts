import { defineComponent } from 'vue'
import InboundEditor from '@/shared/composables/useInboundEditor'
import ComponentSlot from '@/componentSystem/ComponentSlot.vue'

export default defineComponent({ extends: InboundEditor, components: { ComponentSlot } })
