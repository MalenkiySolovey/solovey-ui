import { afterEach, describe, expect, it, vi } from 'vitest'
import RandomUtil from './randomUtil'
import { createClient } from '@/types/clients'

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

function source(words: number[] = [], byte = 255) {
  const queue = [...words]
  const draw = vi.fn((target: Uint32Array | Uint8Array) => {
    if (target instanceof Uint32Array) target.forEach((_, index) => { target[index] = queue.shift() ?? 0 })
    else target.fill(byte)
    return target
  })
  vi.stubGlobal('crypto', { getRandomValues: draw })
  return draw
}

describe('cryptographic random utility bounds', () => {
  it('rejects biased tail draws and keeps alphabet bounds exclusive', () => {
    const draw = source([0xffffffff, 61])
    expect(RandomUtil.randomSeq(1)).toBe('Z')
    expect(draw).toHaveBeenCalledTimes(2)
    source([35])
    expect(RandomUtil.randomLowerAndNum(1)).toBe('z')
    source([255])
    expect(RandomUtil.randomInt(256)).toBe(255)
  })
  it('covers inclusive, reversed and full safe-integer ranges exactly', () => {
    source([4])
    expect(RandomUtil.randomIntRange(7, 3)).toBe(7)
    source([0x003fffff, 0xfffffffe])
    expect(RandomUtil.randomIntRange(Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER)).toBe(Number.MAX_SAFE_INTEGER)
    const draw = source([0xffffffff, 0xffffffff, 0, 0])
    expect(RandomUtil.randomIntRange(Number.MIN_SAFE_INTEGER, Number.MAX_SAFE_INTEGER)).toBe(Number.MIN_SAFE_INTEGER)
    expect(draw).toHaveBeenCalledTimes(2)
    expect(RandomUtil.randomIntRange(0, 0)).toBe(0)
    expect(RandomUtil.randomInt(1)).toBe(0)
  })
  it('validates zero/min/max lengths and never silently shortens credentials', () => {
    source([], 0)
    for (const generator of [(n: number) => RandomUtil.randomSeq(n), (n: number) => RandomUtil.randomLowerAndNum(n), (n: number) => RandomUtil.randomShadowsocksPassword(n)]) {
      expect(generator(0)).toBe('')
      for (const length of [-1, 0.5, NaN, Infinity, 65537]) expect(() => generator(length)).toThrow(RangeError)
    }
    expect(RandomUtil.randomSeq(65536)).toHaveLength(65536)
    expect(atob(RandomUtil.randomShadowsocksPassword(65536))).toHaveLength(65536)
    for (const value of [0, -1, 0.5, NaN, Infinity]) expect(() => RandomUtil.randomInt(value)).toThrow(RangeError)
    expect(() => RandomUtil.randomIntRange(0.5, 10)).toThrow(RangeError)
    expect(() => RandomUtil.randomIntRange(0, Number.MAX_SAFE_INTEGER + 1)).toThrow(RangeError)
  })
  it('chooses one Reality short-ID length per ID and preserves 24 entries and empty default', () => {
    const draw = source(Array(23).fill(7), 255)
    const ids = RandomUtil.randomShortId()
    expect(ids).toHaveLength(24)
    expect(ids[0]).toBe('')
    expect(ids.slice(1).every(id => /^[0-9a-f]{16}$/.test(id))).toBe(true)
    expect(draw.mock.calls.filter(([target]) => target instanceof Uint32Array)).toHaveLength(23)
    expect(draw.mock.calls.filter(([target]) => target instanceof Uint8Array)).toHaveLength(23)
    source([], 0)
    expect(RandomUtil.randomShortId().slice(1).every(id => /^[0-9a-f]{2}$/.test(id))).toBe(true)
  })
  it('preserves UUID version/variant and real client credential default lengths', () => {
    source([], 255)
    const uuid = RandomUtil.randomUUID()
    expect(uuid).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    const client = createClient()
    expect(client.name).toHaveLength(8)
    expect(client.config!.trojan.password).toMatch(/^[0-9a-zA-Z]{10}$/)
    expect(atob(client.config!.shadowsocks16.password)).toHaveLength(16)
    expect(atob(client.config!.shadowsocks.password)).toHaveLength(32)
    expect(client.config!.snell.userkey).toHaveLength(32)
    expect(atob(client.config!.snell.userkey)).toHaveLength(24)
    expect(client.config!.vless.uuid).toMatch(/^.{14}4.{3}-[89ab]/)
    expect(client.config!.trojan.password).not.toContain('undefined')
  })
  it('fails closed when Web Crypto is unavailable', () => {
    vi.stubGlobal('crypto', undefined)
    expect(() => RandomUtil.randomSeq(10)).toThrow()
    expect(() => RandomUtil.randomShadowsocksPassword(24)).toThrow()
  })
})
