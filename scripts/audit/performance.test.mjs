import test from 'node:test'
import assert from 'node:assert/strict'
import { compareBenchmarks, parseBenchmarks, normalizeBenchmarks } from './performance.mjs'

const output=(values,name='BenchmarkRequest-2',owner='example/api')=>'pkg: '+owner+'\n'+values.map(ns=>`${name} 100 ${ns} ns/op 16 B/op 1 allocs/op`).join('\n')+'\nPASS\n'
test('comparison retains all samples and median warnings remain advisory',()=>{
  const result=compareBenchmarks(output([10,10,10,10,10,1000]),output([12,13,14,15,16,17]))
  assert.equal(result.status,'PASS')
  assert.equal(result.policy,'ADVISORY')
  assert.equal(result.regressions.length,1)
  assert.equal(result.comparisons[0].oldNs,10)
  assert.equal(result.comparisons[0].newNs,14.5)
  assert.equal(result.comparisons[0].baselineSamples.length,6)
})
test('missing, malformed, unequal or incomplete observations fail the harness',()=>{
  for(const missing of ['','PASS\n',output([NaN]),'BenchmarkRequest-2 10 5 ns/op'])assert.throws(()=>parseBenchmarks(missing))
  assert.throws(()=>compareBenchmarks(output([1,1,1,1,1,1]),output([1,1,1,1,1,1],'BenchmarkOther-2')),/sets differ/)
  assert.throws(()=>compareBenchmarks(output([1]),output([1])),/Incomplete/)
})
test('package identity separates equal benchmark names',()=>{
  const source=output([1,1,1,1,1,1],'BenchmarkSame-2','example/a')+output([2,2,2,2,2,2],'BenchmarkSame-2','example/b')
  assert.equal(compareBenchmarks(source,source).comparisons.length,2)
})
test('calibration logs between benchmark name and result retain every sample and metric',()=>{
  const raw='goos: linux\ngoarch: amd64\npkg: example/api\ncpu: same runner\n'+[10,11,12,13,14,15].map(ns=>`BenchmarkRequest-2\tfixture initialization\napplication log\n  100 ${ns} ns/op 16 B/op 1 allocs/op`).join('\n')+'\nPASS\n'
  const normalized=normalizeBenchmarks(raw)
  assert.equal(parseBenchmarks(raw).get('example/api/BenchmarkRequest-2').length,6)
  assert.equal(compareBenchmarks(raw,normalized).comparisons[0].newNs,12.5)
  assert.equal(normalized.includes('application log'),false)
  assert.equal(normalized.match(/16 B\/op 1 allocs\/op/g).length,6)
  assert.throws(()=>parseBenchmarks('pkg: example/api\nBenchmarkLost-2\tfixture log\nBenchmarkOther-2 10 5 ns/op'),/Missing benchmark/)
  assert.throws(()=>parseBenchmarks('pkg: example/api\nBenchmarkLost-2\n--- SKIP: BenchmarkLost'),/Missing benchmark/)
})
