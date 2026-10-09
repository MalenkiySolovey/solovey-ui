const alphabet = '0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ'
const maxLength = 65536

function length(value: number): number {
  if (!Number.isSafeInteger(value) || value < 0 || value > maxLength) {
    throw new RangeError('random length must be an integer from 0 to 65536')
  }
  return value
}

// Rejection sampling keeps both small alphabets and the full safe-integer
// interval uniform. Integer arithmetic avoids float rounding at the bounds.
const RandomUtil = {
  randomIntRange(min: number, max: number): number {
    if (!Number.isSafeInteger(min) || !Number.isSafeInteger(max)) {
      throw new RangeError('random bounds must be safe integers')
    }
    if (max < min) [min, max] = [max, min]
    if (min === max) return min
    const span = BigInt(max) - BigInt(min) + 1n
    const words = new Uint32Array(span <= 0x100000000n ? 1 : 2)
    const space = 1n << BigInt(words.length * 32)
    const ceiling = space - space % span
    let sample: bigint
    do {
      globalThis.crypto.getRandomValues(words)
      sample = BigInt(words[0])
      if (words.length === 2) sample = (sample << 32n) | BigInt(words[1])
    } while (sample >= ceiling)
    return Number(BigInt(min) + sample % span)
  },
  randomInt(n: number): number {
    if (!Number.isSafeInteger(n) || n <= 0) throw new RangeError('random bound must be a positive safe integer')
    return this.randomIntRange(0, n - 1)
  },
  randomSeq(count: number): string {
    length(count)
    return Array.from({ length: count }, () => alphabet[this.randomInt(alphabet.length)]).join('')
  },
  randomLowerAndNum(count: number): string {
    length(count)
    return Array.from({ length: count }, () => alphabet[this.randomInt(36)]).join('')
  },
  randomUUID(): string {
    const bytes = new Uint8Array(16)
    globalThis.crypto.getRandomValues(bytes)
    bytes[6] = (bytes[6] & 0x0f) | 0x40
    bytes[8] = (bytes[8] & 0x3f) | 0x80
    const hex = Array.from(bytes, byte => byteToHex[byte])
    return `${hex.slice(0, 4).join('')}-${hex.slice(4, 6).join('')}-${hex.slice(6, 8).join('')}-${hex.slice(8, 10).join('')}-${hex.slice(10).join('')}`
  },
  randomShadowsocksPassword(count: number): string {
    const bytes = new Uint8Array(length(count))
    globalThis.crypto.getRandomValues(bytes)
    let binary = ''
    for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
    return btoa(binary)
  },
  randomShortId(): string[] {
    const shortIds = Array<string>(24).fill('')
    for (let index = 1; index < shortIds.length; index++) {
      const bytes = new Uint8Array(this.randomIntRange(1, 8))
      globalThis.crypto.getRandomValues(bytes)
      shortIds[index] = Array.from(bytes, byte => byteToHex[byte]).join('')
    }
    return shortIds
  },
}

const byteToHex = Array.from({ length: 256 }, (_, byte) => byte.toString(16).padStart(2, '0'))
export default RandomUtil
