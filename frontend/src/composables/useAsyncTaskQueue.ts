import { reactive, ref } from 'vue'

type TaskKey = number | string

const keyOf = (key: TaskKey): string => String(key)

export const runWithConcurrency = async <T>(
  items: T[],
  worker: (item: T) => Promise<void>,
  concurrency = 8,
) => {
  if (items.length === 0) return

  let index = 0
  const workers = Array.from({ length: Math.min(Math.max(1, concurrency), items.length) }, async () => {
    while (index < items.length) {
      const item = items[index]
      index += 1
      await worker(item)
    }
  })

  await Promise.all(workers)
}

export const useAsyncTaskQueue = (concurrency = 8) => {
  const active = reactive<Record<string, boolean>>({})
  const runningAll = ref(false)

  const runOne = async <T>(key: TaskKey, task: () => Promise<T>): Promise<T | undefined> => {
    const normalized = keyOf(key)
    if (active[normalized]) return undefined

    active[normalized] = true
    try {
      return await task()
    } finally {
      active[normalized] = false
    }
  }

  const runMany = async <T>(
    items: T[],
    key: (item: T) => TaskKey,
    task: (item: T) => Promise<void>,
  ) => {
    runningAll.value = true
    try {
      await runWithConcurrency(items, async (item) => {
        await runOne(key(item), () => task(item))
      }, concurrency)
    } finally {
      runningAll.value = false
    }
  }

  return {
    active,
    runningAll,
    runMany,
    runOne,
  }
}
