/// <reference types="node" />

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const source = readFileSync(fileURLToPath(new URL('./Operations.vue', import.meta.url)), 'utf8')

describe('operations cockpit accessibility and mutation boundaries', () => {
  it('uses semantic labelled sections and a bounded live region', () => {
    expect(source).toContain('<nav class="section-nav')
    expect(source.match(/<section id=/g)).toHaveLength(4)
    expect(source.match(/aria-labelledby=/g)?.length).toBeGreaterThanOrEqual(4)
    expect(source).toContain('aria-live="polite"')
    expect(source).toContain('aria-atomic="true"')
    expect(source.match(/tabindex="-1"/g)?.length).toBeGreaterThanOrEqual(3)
  })

  it('keeps focus visible, supports narrow layouts, and disables motion', () => {
    expect(source).toContain(':focus-visible')
    expect(source).toContain('@media (max-width: 600px)')
    expect(source).toContain('@media (prefers-reduced-motion: reduce)')
    expect(source).toContain('scroll-behavior: auto')
  })

  it('does not trigger a sensitive mutation during page load', () => {
    expect(source).toContain('onMounted(() => void loadAll())')
    const loadAllBody = source.slice(source.indexOf('const loadAll = async'), source.indexOf('const runUpdateCheck'))
    expect(loadAllBody).not.toMatch(/checkSignedUpdate|prepareSignedUpdate|activateSignedUpdate|rollbackSignedUpdate|executeDropData|rehearseRestore|executeRestore/)
  })

  it('binds destructive buttons to exact reviewed-state guards', () => {
    expect(source).toContain(':disabled="!canPrepareUpdate"')
    expect(source).toContain(':disabled="!canExecuteDrop"')
    expect(source).toContain(':disabled="!canExecuteRestore"')
    expect(source).toContain('dropPreview.value.blockers.length === 0')
    expect(source).toContain('restoreRehearsal.value?.possible')
  })
})
