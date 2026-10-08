import type { CoreConfigContract } from '@/types/coreConfigContract'

const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value))
const tags = (value: unknown): string[] => typeof value === 'string' ? [value] : Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
const canonical = (value: unknown) => JSON.stringify(value, (_key, item) => item && typeof item === 'object' && !Array.isArray(item) ? Object.fromEntries(Object.keys(item).sort().map(key => [key, item[key]])) : item)

// This is an edit merge, not a migration or a core grammar. Only unreferenced,
// untouched catalogue defaults are removable; operator objects remain intact.
export function mergeTemplateRuleSets(template: Record<string, any>, catalogue: Record<string, any>[], contract: CoreConfigContract): Record<string, any>[] {
  if (!contract.maxRuleNodes || !contract.directHttpClient) throw new Error('editor contract unavailable')
  if (template.rule_set !== undefined && !Array.isArray(template.rule_set)) throw new Error('rule-set collection invalid')
  const queue = [...(template.rules ?? []), ...(template.dns?.rules ?? [])]
  const referenced = new Set<string>()
  for (let cursor = 0; cursor < queue.length; cursor++) {
    if (cursor >= contract.maxRuleNodes) throw new Error('editor traversal budget')
    const rule = queue[cursor]
    for (const tag of tags(rule?.rule_set)) referenced.add(tag)
    if (Array.isArray(rule?.rules)) queue.push(...rule.rules)
  }
  const factory = (item: Record<string, any>) => {
    const result = clone(item)
    // The catalogue belongs to the client template factory, whose existing
    // generator guarantees its simple direct outbound. Never alter stored sets.
    if (result.download_detour === 'direct') {
      result.http_client = clone(contract.directHttpClient)
      delete result.download_detour
    }
    return result
  }
  const entries = catalogue.map(item => ({ old: item, current: factory(item) }))
  const retained = (template.rule_set ?? []).filter((item: Record<string, any>) => {
    if (tags(item?.tag).some(tag => referenced.has(tag))) return true
    return !entries.some(entry => canonical(item) === canonical(entry.old) || canonical(item) === canonical(entry.current))
  })
  const result = clone(retained)
  const seen = new Set<string>(result.flatMap((item: Record<string, any>) => tags(item?.tag)))
  for (const entry of entries) {
    const identities = tags(entry.current.tag)
    if (!identities.some(tag => referenced.has(tag)) || identities.some(tag => seen.has(tag))) continue
    result.push(clone(entry.current))
    for (const tag of identities) seen.add(tag)
  }
  return result
}
