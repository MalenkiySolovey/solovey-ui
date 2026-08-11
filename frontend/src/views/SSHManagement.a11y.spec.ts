/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const source = readFileSync(fileURLToPath(new URL('./SSHManagement.vue', import.meta.url)), 'utf8')

describe('SSH management reconnect safety contract', () => {
  it('exposes an operator-controlled confirmation without retaining proof input', () => {
    expect(source).toContain("candidate.state === 'RECONNECT_REQUIRED'")
    expect(source).toContain('v-model.trim="providerEvidenceRef"')
    expect(source).toContain(':disabled="!canConfirmReconnect"')
    expect(source).toContain('@click="confirmReconnectCandidate"')
    expect(source).toContain("acquireStepUpToken('ssh.candidate.confirm'")
    expect(source).toContain('providerEvidenceRef.value = \'\'')
    expect(source).toContain('confirmCredential.value = \'\'')
  })

  it('does not confirm or roll back during page load or polling', () => {
    const mounted = source.slice(source.indexOf('onMounted(() => {'), source.indexOf('onUnmounted(() => {'))
    expect(mounted).toContain('loadAll()')
    expect(mounted).toContain('refreshCandidate()')
    expect(mounted).not.toContain('confirmReconnectCandidate')
    expect(mounted).not.toContain('rollbackCandidate')
  })

  it('keeps a live announcement, narrow layout, and reduced-motion fallback', () => {
    expect(source).toContain('aria-live="polite"')
    expect(source).toContain('@media (max-width: 600px)')
    expect(source).toContain('@media (prefers-reduced-motion: reduce)')
  })
})
