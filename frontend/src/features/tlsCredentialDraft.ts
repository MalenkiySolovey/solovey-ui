import { shallowReactive, toRaw } from 'vue'

export type CredentialMode = 'path' | 'text' | 'provider'
type Choice = { mode: CredentialMode; text: string[]; path: string[]; provider?: boolean }
const choices = new WeakMap<object, Map<string, Choice>>()

function choicesFor(target: object): Map<string, Choice> {
  const key = toRaw(target)
  let selected = choices.get(key)
  if (!selected) { selected = shallowReactive(new Map<string, Choice>()); choices.set(key, selected) }
  return selected
}

export function selectedCredentialMode(target: object, group: string): CredentialMode | undefined {
  return choicesFor(target).get(group)?.mode
}

// Draft values stay present while switching controls. Only an explicit user
// choice removes inactive credentials from the submitted copy at commit.
export function selectCredentialMode(target: object, group: string, mode: CredentialMode, text: string[], path: string[], provider = false) {
  choicesFor(target).set(group, { mode, text, path, provider })
}

export function serializeCredentialDraft<T>(value: T): T {
  if (Array.isArray(value)) return value.map(item => serializeCredentialDraft(item)) as T
  if (!value || typeof value !== 'object' || value instanceof Date) return value
  const source = value as Record<string, unknown>
  const copy: Record<string, unknown> = {}
  for (const [key, child] of Object.entries(source)) copy[key] = serializeCredentialDraft(child)
  for (const selection of choicesFor(source).values()) {
    for (const key of selection.mode === 'text' ? selection.path : selection.text) delete copy[key]
    if (selection.mode === 'provider') for (const key of selection.path) delete copy[key]
    if (selection.provider && selection.mode !== 'provider') { delete copy.certificate_provider; delete copy.acme }
  }
  return copy as T
}
