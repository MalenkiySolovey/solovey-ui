import axios, { type InternalAxiosRequestConfig } from 'axios'
import { describe, expect, it } from 'vitest'

const serialize = async (data: unknown) => {
  let request: InternalAxiosRequestConfig | undefined
  const client = axios.create({
    headers: { post: { 'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8' } },
    adapter: async config => {
      request = config
      return { data: {}, status: 200, statusText: 'OK', headers: {}, config }
    },
  })
  await client.post('/api/form-fixture', data)
  return request
}

describe('HTTP form serialization', () => {
  it('preserves the panel nested-field and array wire format', async () => {
    const request = await serialize({ nested: { value: 'two words' }, items: ['one', 'two'] })
    expect(request?.method).toBe('post')
    expect(request?.data).toBe('nested%5Bvalue%5D=two+words&items%5B%5D=one&items%5B%5D=two')
  })

  it('ignores inherited serializer options without changing the wire format', async () => {
    const previousDepth = Object.getOwnPropertyDescriptor(Object.prototype, 'maxDepth')
    const previousVisitor = Object.getOwnPropertyDescriptor(Object.prototype, 'visitor')
    let foreignVisitorCalled = false
    Object.defineProperty(Object.prototype, 'maxDepth', { configurable: true, value: 0 })
    Object.defineProperty(Object.prototype, 'visitor', {
      configurable: true,
      value: () => { foreignVisitorCalled = true; return false },
    })
    try {
      const request = await serialize({ nested: { value: 'two words' }, items: ['one', 'two'] })
      expect(foreignVisitorCalled).toBe(false)
      expect(request?.data).toBe('nested%5Bvalue%5D=two+words&items%5B%5D=one&items%5B%5D=two')
    } finally {
      for (const [name, previous] of [['maxDepth', previousDepth], ['visitor', previousVisitor]] as const) {
        if (previous) Object.defineProperty(Object.prototype, name, previous)
        else Reflect.deleteProperty(Object.prototype, name)
      }
    }
  })
})
