export type StrictSelectValue = string | number | boolean | null | undefined
export type StrictSelectModel = StrictSelectValue | StrictSelectValue[]
export type StrictSelectItem = StrictSelectValue | object

export interface StrictSelectItemOptions {
  itemTitle: string
  itemValue: string
}

export interface StrictSelectUpdateOptions extends StrictSelectItemOptions {
  multiple: boolean
}

export const strictSelectItemValueOf = (
  item: StrictSelectItem,
  options: StrictSelectItemOptions,
): StrictSelectValue => {
  if (item && typeof item === 'object') {
    const record = item as Record<string, unknown>
    const value = record[options.itemValue]
    return (value ?? record[options.itemTitle]) as StrictSelectValue
  }

  return item
}

export const strictSelectAllowedValues = (
  items: StrictSelectItem[],
  options: StrictSelectItemOptions,
) => new Set(items.map(item => strictSelectItemValueOf(item, options)))

export const sanitizeStrictSelectMultiple = (
  value: unknown,
  allowedValues: Set<StrictSelectValue>,
  options: StrictSelectItemOptions,
): StrictSelectValue[] => {
  const raw = Array.isArray(value) ? value : []
  const seen = new Set<StrictSelectValue>()
  const next: StrictSelectValue[] = []

  for (const item of raw) {
    const itemValue = strictSelectItemValueOf(item as StrictSelectItem, options)
    if (!allowedValues.has(itemValue) || seen.has(itemValue)) continue
    seen.add(itemValue)
    next.push(itemValue)
  }

  return next
}

export const sanitizeStrictSelectSingle = (
  value: unknown,
  allowedValues: Set<StrictSelectValue>,
  options: StrictSelectItemOptions,
): StrictSelectValue => {
  const itemValue = strictSelectItemValueOf(value as StrictSelectItem, options)
  if (itemValue === null || itemValue === undefined || itemValue === '') return itemValue
  return allowedValues.has(itemValue) ? itemValue : undefined
}

export const sanitizeStrictSelectModel = (
  value: unknown,
  allowedValues: Set<StrictSelectValue>,
  options: StrictSelectUpdateOptions,
): StrictSelectModel => {
  return options.multiple
    ? sanitizeStrictSelectMultiple(value, allowedValues, options)
    : sanitizeStrictSelectSingle(value, allowedValues, options)
}

export const strictSelectModelsEqual = (
  left: StrictSelectModel,
  right: StrictSelectModel,
): boolean => {
  if (Array.isArray(left) || Array.isArray(right)) {
    if (!Array.isArray(left) || !Array.isArray(right)) return false
    return left.length === right.length && left.every((item, index) => item === right[index])
  }
  return left === right
}

export const sanitizeStrictSelectUpdate = (
  value: unknown,
  currentValue: unknown,
  allowedValues: Set<StrictSelectValue>,
  options: StrictSelectUpdateOptions,
): StrictSelectModel => {
  if (!options.multiple) return sanitizeStrictSelectSingle(value, allowedValues, options)
  if (value === null || value === undefined || value === '') return []
  if (!Array.isArray(value)) return sanitizeStrictSelectMultiple(currentValue, allowedValues, options)

  const next = sanitizeStrictSelectMultiple(value, allowedValues, options)
  const hasUnknown = value.some(item => !allowedValues.has(strictSelectItemValueOf(item as StrictSelectItem, options)))
  if (hasUnknown && next.length === 0) {
    return sanitizeStrictSelectMultiple(currentValue, allowedValues, options)
  }
  return next
}
