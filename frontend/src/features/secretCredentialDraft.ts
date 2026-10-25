export interface SecretReference {
  owner: string
  id: string
  policy: 'OWNER_USE_AND_PRESENCE'
  projection: 'MASKED'
}

export interface SecretView {
  reference: SecretReference
  present: boolean
  rotationRevision?: string
}

export type SecretMutation =
  | { intent: 'unchanged'; reference: SecretReference; present: boolean; rotationRevision: string }
  | { intent: 'replace'; value: string }
  | { intent: 'clear' }

// UI metadata is a draft hint. The owning save operation independently checks
// its row identity, exact reference, revision, capability and permission.
export function unchangedSecret(value: unknown): Extract<SecretMutation, { intent: 'unchanged' }> | undefined {
  if (!value || typeof value !== 'object') return undefined
  const data = value as Record<string, unknown>
  if (data.intent !== undefined && data.intent !== 'unchanged') return undefined
  if ('value' in data || (data.intent === 'unchanged' && Object.keys(data).some(key => !['intent', 'reference', 'present', 'rotationRevision'].includes(key)))) return undefined
  if (!data.reference || typeof data.reference !== 'object' || typeof data.present !== 'boolean') return undefined
  const reference = data.reference as Record<string, unknown>
  if (Object.keys(reference).some(key => !['owner', 'id', 'policy', 'projection'].includes(key))) return undefined
  if (typeof reference.owner !== 'string' || !reference.owner || typeof reference.id !== 'string' || !reference.id ||
      reference.policy !== 'OWNER_USE_AND_PRESENCE' || reference.projection !== 'MASKED' ||
      (data.rotationRevision !== undefined && (typeof data.rotationRevision !== 'string' ||
        (data.rotationRevision !== '' && !/^[a-f0-9]{64}$/.test(data.rotationRevision))))) return undefined
  return { intent: 'unchanged', reference: { owner: reference.owner, id: reference.id,
    policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }, present: data.present,
    rotationRevision: (data.rotationRevision as string | undefined) ?? '' }
}

export function secretMutation(value: unknown): SecretMutation | undefined {
  if (value === undefined) return undefined
  const unchanged = unchangedSecret(value)
  if (unchanged) return unchanged
  if (value && typeof value === 'object') {
    const data = value as Record<string, unknown>
    if (data.intent === 'clear' && Object.keys(data).length === 1) return { intent: 'clear' }
    if (data.intent === 'replace' && Object.keys(data).length === 2 && typeof data.value === 'string' &&
        data.value.length > 0 && new TextEncoder().encode(data.value).length <= 1 << 20 && !data.value.startsWith('sbox:v1:')) {
      return { intent: 'replace', value: data.value }
    }
  }
  throw new Error('CLIENT_CREDENTIAL_INVALID')
}
