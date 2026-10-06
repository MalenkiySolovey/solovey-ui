import test from 'node:test'
import assert from 'node:assert/strict'
import { compareBenchmarks, parseBenchmarks } from './performance.mjs'

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
