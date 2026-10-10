// The traffic column displays used upload + download; quota is independent.
export const usedTraffic = (client: { up: number; down: number }): number => client.up + client.down

export const compareUsedTraffic = (a: { up: number; down: number }, b: { up: number; down: number }): number =>
  usedTraffic(a) - usedTraffic(b)
