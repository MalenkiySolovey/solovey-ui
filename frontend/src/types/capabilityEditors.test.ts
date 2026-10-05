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
 it('keeps same-type history editable without allowing a new unavailable type',()=>{
  expect(capabilityEditState(snapshot,'outbounds','naive','naive')).toMatchObject({allowed:true,message:'capability.historical'})
  expect(capabilityEditState(snapshot,'outbounds','naive','direct').allowed).toBe(false)
  const choices=capabilityTypeChoices(snapshot,'outbounds',{Direct:'direct',Naive:'naive'},'unknown')
  expect(choices.find(choice=>choice.value==='direct')?.props.disabled).toBe(false)
  expect(choices.find(choice=>choice.value==='naive')?.props.disabled).toBe(true)
  expect(choices.find(choice=>choice.value==='unknown')).toMatchObject({title:'unknown',props:{disabled:true}})
  expect(snapshot.facts).toHaveLength(3)
 })
})
