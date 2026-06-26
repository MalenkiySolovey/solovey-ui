import { describe, expect, it } from 'vitest'

import {
  sanitizeStrictSelectUpdate,
  strictSelectAllowedValues,
} from '@/shared/models/strictSelectModel'

const options = {
  itemTitle: 'title',
  itemValue: 'value',
  multiple: true,
}

describe('strictSelectModel', () => {
  it('accepts Vuetify item objects when selecting existing values', () => {
    const allowed = strictSelectAllowedValues([{ title: 'Inbound', value: 'inbound-1' }], options)

    expect(sanitizeStrictSelectUpdate(
      [{ title: 'Inbound', value: 'inbound-1' }],
      [],
      allowed,
      options,
    )).toEqual(['inbound-1'])
  })

  it('keeps the current value when Enter submits a non-existing search term', () => {
    const allowed = strictSelectAllowedValues(['inbound-1'], options)

    expect(sanitizeStrictSelectUpdate(
      ['definitely-not-existing'],
      ['inbound-1'],
      allowed,
      options,
    )).toEqual(['inbound-1'])
  })

  it('allows explicit clear and chip removal', () => {
    const allowed = strictSelectAllowedValues(['inbound-1'], options)

    expect(sanitizeStrictSelectUpdate(null, ['inbound-1'], allowed, options)).toEqual([])
    expect(sanitizeStrictSelectUpdate([], ['inbound-1'], allowed, options)).toEqual([])
  })
})
