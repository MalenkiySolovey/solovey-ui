// Presentation types for the pinned per-consumer contract. Backend facts and
// validation determine which controls are available and accepted.
export type MemoryQuantity = number | string

export interface HTTP2Tuning {
  idle_timeout?: string
  keep_alive_period?: string
  stream_receive_window?: MemoryQuantity
  connection_receive_window?: MemoryQuantity
  max_concurrent_streams?: number
}

export interface QUICTuning extends HTTP2Tuning {
  initial_packet_size?: number
  disable_path_mtu_discovery?: boolean
}
