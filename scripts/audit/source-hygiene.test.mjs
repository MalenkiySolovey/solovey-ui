import test from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { validateGoSource, validateUnixText } from './source-hygiene.mjs'

test('owned Unix text rejects CRLF, bare CR and BOM',() => {
  validateUnixText(Buffer.from('#!/bin/sh\necho ready\n'),'entrypoint')
  for(const bytes of [Buffer.from('#!/bin/sh\r\n'),Buffer.from('echo\rbroken'),Buffer.from('\ufeff#!/bin/sh\n')]) {
    assert.throws(() => validateUnixText(bytes,'entrypoint'))
  }
})
test('gofmt accepts canonical Go and rejects unformatted or invalid source',() => {
  validateGoSource('package fixture\n\nfunc Ready() bool { return true }\n','fixture.go')
  assert.throws(() => validateGoSource('package fixture\nfunc Ready()bool{return true}\n','fixture.go'),/gofmt required/)
  assert.throws(() => validateGoSource('package fixture\nfunc {\n','fixture.go'),/Invalid Go/)
})
test('owned shell syntax rejects a malformed entrypoint',{skip:process.platform==='win32'},() => {
  const result=spawnSync('bash',['-n'],{input:'#!/usr/bin/env bash\nif true; then\n',encoding:'utf8'})
  assert.equal(result.status,2)
})
