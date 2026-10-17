import assert from 'node:assert/strict'
import test from 'node:test'
import { inspectPolicy, unreviewedFrontendPolicy } from './architecture-policy.mjs'

test('new omitted-final Direct fallback requires policy review', () => {
  for (const source of ["const final = data.final || 'direct'", "const final = data.final ?? 'direct'", "const final = data.final ? data.final : 'direct'"]) {
    const found = inspectPolicy(source, 'policy.ts')
    assert.equal(found.length, 1)
    assert.equal(unreviewedFrontendPolicy(found.map(expression => ({ path: 'frontend/src/new.ts', expression }))).length, 1)
  }
})

test('Vue script fallbacks are parsed; explicit selections and comments are permitted', () => {
  assert.deepEqual(inspectPolicy("<script setup lang=\"ts\">const final = data.final || 'direct'</script><template><div /></template>", 'Policy.vue'), ["data.final || 'direct'"])
  assert.deepEqual(inspectPolicy("// final || 'direct'\nconst choices = ['direct', 'reject']; const final = 'direct'", 'policy.ts'), [])
})

test('legacy exception has exact path, expression and multiplicity; reduction is allowed', () => {
  const legacy = { path: 'frontend/src/components/protocols/Failover.vue', expression: "this.$props.data.final || 'direct'" }
  assert.deepEqual(unreviewedFrontendPolicy([legacy]), [])
  assert.deepEqual(unreviewedFrontendPolicy([]), [])
  assert.deepEqual(unreviewedFrontendPolicy([legacy, legacy]), [legacy])
  assert.equal(unreviewedFrontendPolicy([{ ...legacy, expression: "other.final || 'direct'" }]).length, 1)
})
