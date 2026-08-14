import { defineComponent } from 'vue'
import InboundEditor from '@/features/useInboundEditor'
import ComponentSlot from '@/componentSystem/ComponentSlot.vue'

export default defineComponent({ extends: InboundEditor, components: { ComponentSlot } })
