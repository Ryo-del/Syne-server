import { invoke } from '@tauri-apps/api/core'

export type ServerConfig = {
  name: string
  http_port: number
  p2p_port: number
  files_path: string
  autostart: boolean
  monitor_interval_sec?: number
  id_digits?: number
  claim_code_length?: number
}

let cached: Promise<ServerConfig | null> | null = null

export function getConfig() {
  return (cached ??= invoke<ServerConfig | null>('load_config').catch(() => null))
}
export function resetConfig() {
  cached = null
}
export async function apiBase() {
  const c = await getConfig()
  return `http://localhost:${c?.http_port ?? 8080}`
}