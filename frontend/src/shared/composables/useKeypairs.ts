import HttpUtils, { type Msg } from '@/plugins/httputil'

export interface TLSKeypair {
  privateKey: string[]
  certificate: string[]
}

export interface RealityKeypair {
  privateKey?: string
  publicKey?: string
}

export interface ECHKeypair {
  config: string[]
  key: string[]
}

export const generateKeypair = (kind: 'ech' | 'tls' | 'reality' | 'wireguard', options?: string): Promise<Msg> => {
  return HttpUtils.get('api/keypairs', { k: kind, ...(options == null ? {} : { o: options }) })
}

const collectPEMSections = (lines: string[], sections: Record<string, [string, string]>): Record<string, string[]> => {
  const result = Object.fromEntries(Object.keys(sections).map(key => [key, [] as string[]]))
  let active = ''
  for (const line of lines) {
    for (const [key, [begin, end]] of Object.entries(sections)) {
      if (line === begin) active = key
      if (active === key) result[key].push(line)
      if (line === end && active === key) active = ''
    }
  }
  return result
}

export const parseTLSKeypair = (lines: string[]): TLSKeypair => {
  const sections = collectPEMSections(lines, {
    privateKey: ['-----BEGIN PRIVATE KEY-----', '-----END PRIVATE KEY-----'],
    certificate: ['-----BEGIN CERTIFICATE-----', '-----END CERTIFICATE-----'],
  })
  return { privateKey: sections.privateKey, certificate: sections.certificate }
}

export const parseRealityKeypair = (lines: string[]): RealityKeypair => {
  const result: RealityKeypair = {}
  for (const line of lines) {
    if (line.startsWith('PrivateKey: ')) result.privateKey = line.slice('PrivateKey: '.length)
    if (line.startsWith('PublicKey: ')) result.publicKey = line.slice('PublicKey: '.length)
  }
  return result
}

export const parseECHKeypair = (lines: string[]): ECHKeypair => {
  const sections = collectPEMSections(lines, {
    config: ['-----BEGIN ECH CONFIGS-----', '-----END ECH CONFIGS-----'],
    key: ['-----BEGIN ECH KEYS-----', '-----END ECH KEYS-----'],
  })
  return { config: sections.config, key: sections.key }
}
