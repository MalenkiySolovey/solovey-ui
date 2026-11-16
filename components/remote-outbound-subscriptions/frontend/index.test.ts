import {beforeEach,describe,expect,it,vi} from 'vitest'
const contributions=vi.hoisted(()=>({registerComponent:vi.fn()}))
vi.mock('@solovey/component-ui/contributions',()=>contributions)
import {register} from './index'
describe('Remote source contribution',()=>{
 beforeEach(()=>contributions.registerComponent.mockClear())
 it('retains routes, settings/client slots and all six locales',async()=>{
  register()
  expect(contributions.registerComponent).toHaveBeenCalledTimes(1)
  const contribution=contributions.registerComponent.mock.calls[0]![0]
  expect(contribution.id).toBe('remote-outbound-subscriptions')
  expect(contribution.apiVersion).toBe('1.0')
  expect(contribution.routes[0]).toMatchObject({path:'/remote-subscriptions',alias:'/remote-outbound-subscriptions'})
  expect(contribution.nav[0]).toMatchObject({path:'/remote-subscriptions',order:35,singBoxSettings:true})
  expect(contribution.slots.map((entry:{slot:string})=>entry.slot)).toEqual(['settings:subscription-advanced','client-editor:subscription-sources'])
  expect(Object.keys(contribution.locales).sort()).toEqual(['en','fa','ru','vi','zhHans','zhHant'])
  for(const load of Object.values(contribution.locales) as Array<()=>Promise<{default:unknown}>>) expect((await load()).default).toBeTruthy()
 })
})
