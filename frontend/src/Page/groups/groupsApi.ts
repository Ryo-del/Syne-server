import { apiBase } from '../../api'

export type Group = { id: number; name: string; emoji: string; members: number }
export type Member = { login: string; fname: string; sname: string; claimed: boolean }
export type ApiUser = {
  Login: string
  FName: string
  SName: string
  Role: string
  GroupID: number
  Group: string
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch((await apiBase()) + path, init)
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new Error(data?.error || `Ошибка сервера (${res.status})`)
  return data as T
}

const json = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
})

export const listGroups = () => request<Group[]>('/api/groups')
export const listUsers = () => request<ApiUser[]>('/api/users')
export const createGroup = (name: string, emoji: string) =>
  request<Group>('/api/groups', json('POST', { name, emoji }))
export const setGroupEmoji = (id: number, emoji: string) =>
  request<Group>(`/api/groups/${id}`, json('PATCH', { emoji }))
export const deleteGroup = (id: number) =>
  request<{ ok: boolean }>(`/api/groups/${id}`, { method: 'DELETE' })
export const listMembers = (id: number) => request<Member[]>(`/api/groups/${id}/members`)
export const addMembers = (id: number, logins: string[]) =>
  request<{ added: number; skipped: number }>(`/api/groups/${id}/members`, json('POST', { logins }))
export const removeMembers = (id: number, logins: string[]) =>
  request<{ removed: number }>(`/api/groups/${id}/members`, json('DELETE', { logins }))

export function plural(n: number, one: string, few: string, many: string) {
  const m10 = n % 10
  const m100 = n % 100
  if (m10 === 1 && m100 !== 11) return one
  if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return few
  return many
}