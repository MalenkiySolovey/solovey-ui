import { toRaw } from 'vue'

// Per editor instance; never serialized into the draft. Distinct objects may
// legitimately carry the same address, while edits/reorders preserve identity.
export function createInboundAddressKey() {
  const keys = new WeakMap<object, number>()
  let sequence = 0
  return (address: object): number => {
    const raw = toRaw(address)
    let key = keys.get(raw)
    if (key === undefined) { key = ++sequence; keys.set(raw, key) }
    return key
  }
}
