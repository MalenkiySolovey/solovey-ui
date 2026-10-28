import { describe, expect, it } from 'vitest'
import { capabilityEditState, capabilityTypeChoices } from './capabilityEditors'
import type { RuntimeCapabilities } from './runtimeCapabilities'

const snapshot: RuntimeCapabilities = {
 schema: 'solovey-ui/entity-capabilities/v1', componentProfile: 'minimal', facts: [
  { category:'outbounds', type:'direct',runtimeType:'direct',known:true,contextSupported:true,registered:true,compiled:true,available:true },
  { category:'outbounds', type:'naive',runtimeType:'naive',known:true,contextSupported:true,registered:false,compiled:false,available:false,reason:'KNOWN_BUT_NOT_COMPILED' },
  { category:'endpoints', type:'warp',runtimeType:'wireguard',known:true,contextSupported:true,registered:true,compiled:true,available:true },
 ],
}
describe('capability editor projection',()=>{
 it('fails closed without facts and uses only backend context/availability',()=>{
  expect(capabilityEditState(undefined,'outbounds','direct').allowed).toBe(false)
  expect(capabilityEditState(snapshot,'outbounds','direct').allowed).toBe(true)
  expect(capabilityEditState(snapshot,'outbounds','naive').allowed).toBe(false)
  expect(capabilityEditState(snapshot,'endpoints','warp').allowed).toBe(true)
  expect(capabilityEditState(snapshot,'inbounds','warp','warp').allowed).toBe(false)
  expect(capabilityEditState(snapshot,'outbounds','unknown','unknown').allowed).toBe(false)
 })
 it('separates preserved inspection from available creation and reactivates from new facts',()=>{
  expect(capabilityEditState(snapshot,'outbounds','naive','naive')).toMatchObject({allowed:false,historical:true,message:'capability.historical'})
  expect(capabilityEditState(snapshot,'outbounds','naive','direct').allowed).toBe(false)
  const catalog={Direct:'direct',Naive:'naive',Unknown:'unknown'}
  expect(capabilityTypeChoices(undefined,'outbounds',catalog)).toEqual([])
  expect(capabilityTypeChoices(snapshot,'outbounds',catalog)).toEqual([{title:'Direct',value:'direct'}])
  const supporting=structuredClone(snapshot)
  Object.assign(supporting.facts[1], { available:true, compiled:true, registered:true })
  expect(capabilityEditState(supporting,'outbounds','naive','naive')).toEqual({allowed:true})
  expect(capabilityTypeChoices(supporting,'outbounds',catalog).map(choice=>choice.value)).toEqual(['direct','naive'])
  expect(snapshot.facts).toHaveLength(3)
 })
 it('treats unavailable nested transports as inactive history without changing the draft',()=>{
  const supporting=structuredClone(snapshot)
  supporting.facts.push({...supporting.facts[0],type:'vless',runtimeType:'vless'})
  const transport={type:'mkcp',seed:{present:true,revision:'unchanged'},future:{value:42}}
  const original=structuredClone(transport)
  expect(capabilityEditState(supporting,'outbounds','vless','vless',transport,{mkcp:{available:false}})).toMatchObject({allowed:false,historical:true,reason:'TRANSPORT_CAPABILITY_UNAVAILABLE'})
  expect(capabilityEditState(supporting,'outbounds','vless','vless',transport,{mkcp:{available:true}})).toEqual({allowed:true})
  expect(transport).toEqual(original)
 })
})
