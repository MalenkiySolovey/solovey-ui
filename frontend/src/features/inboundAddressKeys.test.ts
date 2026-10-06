import { describe, expect, it } from 'vitest'
import { createRenderer, defineComponent, h, nextTick, reactive, ref } from 'vue'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createInboundAddressKey } from './inboundAddressKeys'

describe('inbound address row identity', () => {
  it('retains actual Vue child state across duplicate-address edit/reorder/delete without draft identity fields', async () => {
    const key = createInboundAddressKey()
    const a = { server: 'same', server_port: 443 }
    const b = { ...a }
    const rows = ref([a, b])
    const instanceIds: number[] = []
    let instance = 0
    const Row = defineComponent({ props: ['address'], setup() { const id = ++instance; instanceIds.push(id); return () => h('span', { instance: id }) } })
    const renderer = createRenderer<any, any>({
      createElement: (tag) => ({ tag, children: [], props: {} }), createText: text => ({ text }), createComment: text => ({ text }),
      insert: (node, parent, anchor) => { if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1); const index = anchor ? parent.children.indexOf(anchor) : -1; parent.children.splice(index < 0 ? parent.children.length : index, 0, node); node.parent = parent },
      remove: node => { node.parent?.children.splice(node.parent.children.indexOf(node), 1); node.parent = null },
      setText: (node, text) => { node.text = text }, setElementText: (node, text) => { node.text = text },
      parentNode: node => node.parent, nextSibling: node => node.parent?.children[node.parent.children.indexOf(node) + 1], patchProp: (node, name, _previous, value) => { node.props[name] = value },
    })
    const root: any = { children: [] }
    const app = renderer.createApp({ render: () => h('div', rows.value.map(address => h(Row, { key: key(address), address }))) })
    app.mount(root)
    const rendered = () => root.children[0].children.map((node: any) => node.props.instance)
    expect(rendered()).toEqual([1, 2])
    expect(key(reactive(a))).toBe(key(a))
    expect(key(a)).not.toBe(key(b))
    rows.value[0].server = 'edited'
    rows.value.reverse()
    await nextTick()
    expect(rendered()).toEqual([2, 1])
    rows.value.splice(0, 1)
    await nextTick()
    expect(rendered()).toEqual([1])
    rows.value.push({ ...b })
    await nextTick()
    expect(rendered()).toEqual([1, 3])
    expect(instanceIds).toEqual([1, 2, 3])
    expect(Object.keys(rows.value[0])).toEqual(['server', 'server_port'])
    app.unmount()
  })
  it('both rendered lists consume the editor key instead of index/value identity', () => {
    for (const file of ['../layouts/modals/Inbound.vue', '../components/nexus/drawers/InboundDrawer.vue']) expect(readFileSync(fileURLToPath(new URL(file, import.meta.url)), 'utf8')).toContain(':key="addressKey(addr)"')
  })
})
