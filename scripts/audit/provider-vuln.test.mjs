import test from 'node:test'
import assert from 'node:assert/strict'
import { auditProviders } from './provider-vuln.mjs'

function audit(results, env = {}) {
  const calls = []
  const status = auditProviders({
    env, log() {},
    scan(command, args, options) {
      calls.push({ command, args, options })
      return results[calls.length - 1]
    },
  })
  return { status, calls }
}

test('Extended findings fail the audit after a clean Official scan', () => {
  const { status, calls } = audit([{ status: 0 }, { status: 3 }], {
    GOFLAGS: '-mod=mod -modfile=untrusted.mod -tags=untrusted', GOMAXPROCS: '2',
  })
  assert.equal(status, 3)
  assert.equal(calls.length, 2)
  assert.equal(calls[0].options.env.GOFLAGS, '-mod=readonly -modfile=go.mod')
  assert.equal(calls[1].options.env.GOFLAGS, '-mod=readonly -modfile=go.extended.mod')
  for (const call of calls) {
    assert.equal(call.command, 'govulncheck')
    assert.equal(call.args[0], '-tags')
    assert.equal(call.args.at(-1), './...')
    assert.equal(call.options.env.GOMAXPROCS, '2')
    assert.equal(call.options.env.GOMEMLIMIT, '4GiB')
    assert(call.args[1].split(',').includes('with_quic'))
    assert(!call.args[1].includes('untrusted'))
  }
  assert(!calls[0].args[1].split(',').includes('shtorm_extended'))
  for (const tag of ['shtorm_extended', 'with_gvisor', 'with_openconnect', 'with_openvpn']) {
    assert(calls[1].args[1].split(',').includes(tag))
  }
})

test('Official findings are retained and Extended is still scanned', () => {
  const { status, calls } = audit([{ status: 3 }, { status: 0 }])
  assert.equal(status, 3)
  assert.equal(calls.length, 2)
})

test('scanner execution failures fail closed for either provider', () => {
  for (const failure of [{ error: Error('scanner missing'), status: null }, { signal: 'SIGTERM', status: null }, {}]) {
    for (const index of [0, 1]) {
      const results = [{ status: 0 }, { status: 0 }]
      results[index] = failure
      const { status, calls } = audit(results)
      assert.equal(status, 1)
      assert.equal(calls.length, 2)
    }
  }
})

test('only successful scans of both canonical provider graphs pass', () => {
  assert.equal(audit([{ status: 0 }, { status: 0 }]).status, 0)
  const { calls } = audit([{ status: 0 }, { status: 0 }], { GOMEMLIMIT: '3GiB' })
  assert(calls.every(call => call.options.env.GOMEMLIMIT === '3GiB'))
})
