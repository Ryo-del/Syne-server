import { useEffect, useState } from 'react'
import { invoke } from '@tauri-apps/api/core'
import { open, save } from '@tauri-apps/plugin-dialog'
import { apiBase } from '../../api'
import './Settings.css'

type Form = {
  http_port: string
  p2p_port: string
  monitor_interval_sec: string
  id_digits: string
  claim_code_length: string
}
type Msg = { kind: 'ok' | 'err'; text: string } | null

async function api(path: string, init?: RequestInit) {
  const res = await fetch(`${await apiBase()}${path}`, init)
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    throw new Error(body?.error ?? `HTTP ${res.status}`)
  }
  return res
}

export default function Settings() {
  const [form, setForm] = useState<Form | null>(null)
  const [msg, setMsg] = useState<Msg>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api('/api/settings')
      .then((r) => r.json())
      .then((s) =>
        setForm({
          http_port: String(s.http_port),
          p2p_port: String(s.p2p_port),
          monitor_interval_sec: String(s.monitor_interval_sec),
          id_digits: String(s.id_digits),
          claim_code_length: String(s.claim_code_length),
        }),
      )
      .catch((e) => setMsg({ kind: 'err', text: e.message }))
  }, [])

  const set = (k: keyof Form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => f && { ...f, [k]: e.target.value.replace(/\D/g, '') })

  const run = async (fn: () => Promise<string | void>) => {
    setBusy(true)
    setMsg(null)
    try {
      const text = await fn()
      if (text) setMsg({ kind: 'ok', text })
    } catch (e) {
      setMsg({ kind: 'err', text: e instanceof Error ? e.message : String(e) })
    } finally {
      setBusy(false)
    }
  }

  const saveSettings = () =>
    run(async () => {
      if (!form) return
      const res = await api('/api/settings', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          http_port: Number(form.http_port),
          p2p_port: Number(form.p2p_port),
          monitor_interval_sec: Number(form.monitor_interval_sec),
          id_digits: Number(form.id_digits),
          claim_code_length: Number(form.claim_code_length),
        }),
      })
      const data = await res.json()
      return data.restart_required
        ? 'Сохранено. Порты изменятся после перезапуска сервера'
        : 'Сохранено'
    })

  const exportUsers = () =>
    run(async () => {
      const text = await (await api('/api/users/export')).text()
      const path = await save({
        defaultPath: 'students.csv',
        filters: [{ name: 'CSV', extensions: ['csv'] }],
      })
      if (!path) return
      // BOM нужен, чтобы Excel правильно прочитал кириллицу
      await invoke('write_text_file', { path, content: '\uFEFF' + text })
      return 'Список экспортирован'
    })

  const importUsers = () =>
    run(async () => {
      const path = await open({
        multiple: false,
        filters: [{ name: 'CSV', extensions: ['csv', 'txt'] }],
      })
      if (typeof path !== 'string') return
      const content = await invoke<string>('read_text_file', { path })
      const res = await api('/api/users/import', {
        method: 'POST',
        headers: { 'Content-Type': 'text/csv' },
        body: content,
      })
      const r: { created: number; failed: { line: number; reason: string }[] } = await res.json()
      let text = `Добавлено: ${r.created}`
      if (r.failed?.length) {
        const first = r.failed.slice(0, 3).map((f) => `строка ${f.line}: ${f.reason}`).join('; ')
        text += `, пропущено: ${r.failed.length} (${first})`
      }
      return text
    })
    const pickClientAddr = (raw: string): string | null => {
    const addrs = raw
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l.startsWith('addr='))
      .map((l) => l.slice('addr='.length).trim())
      .filter((a) => a && !a.startsWith('/ip4/127.'))

    const rank = (a: string) => {
      if (/^\/ip4\/192\.168\./.test(a)) return 0
      if (/^\/ip4\/10\./.test(a)) return 1
      if (/^\/ip4\/172\.(1[6-9]|2\d|3[01])\./.test(a)) return 2
      return 3
    }

    return [...addrs].sort((a, b) => rank(a) - rank(b))[0] ?? null
  }
  const createClientConfig = () =>
    run(async () => {
      const raw = await (await api('/api/client-config')).text()
      const addr = pickClientAddr(raw)
      if (!addr) throw new Error('Сервер не вернул ни одного адреса')

      const path = await save({
        defaultPath: 'client-config.txt',
        filters: [{ name: 'Text', extensions: ['txt'] }],
      })
      if (!path) return
      await invoke('write_text_file', { path, content: addr + '\n' })
      return `client-config.txt создан (${addr})`
    })

  if (!form) return <div className="settings">{msg ? <div className="msg err">{msg.text}</div> : null}</div>

  return (
    <div className="settings">
      <h1>Настройки</h1>

      <section className="card">
        <h2>Мониторинг</h2>
        <label>
          Время между запросами, сек (1–60)
          <input value={form.monitor_interval_sec} inputMode="numeric" onChange={set('monitor_interval_sec')} />
        </label>
      </section>

      <section className="card">
        <h2>Список учеников</h2>
        <div className="row">
          <label>
            Цифр в случайном id (4–12)
            <input value={form.id_digits} inputMode="numeric" onChange={set('id_digits')} />
          </label>
          <label>
            Символов в ClaimCode (6–32)
            <input value={form.claim_code_length} inputMode="numeric" onChange={set('claim_code_length')} />
          </label>
        </div>
        <div className="row">
          <button className="secondary" onClick={importUsers} disabled={busy}>Импортировать список</button>
          <button className="secondary" onClick={exportUsers} disabled={busy}>Экспортировать список</button>
        </div>
        <p className="hint">CSV: login;fname;sname;role. Логин можно оставить пустым, он сгенерируется.</p>
      </section>

      <section className="card">
        <h2>Сеть</h2>
        <div className="row">
          <label>
            HTTP порт
            <input value={form.http_port} inputMode="numeric" onChange={set('http_port')} />
          </label>
          <label>
            P2P порт
            <input value={form.p2p_port} inputMode="numeric" onChange={set('p2p_port')} />
          </label>
        </div>
        <button className="secondary" onClick={createClientConfig} disabled={busy}>
          Создать client-config.txt
        </button>
      </section>

      {msg && <div className={`msg ${msg.kind}`}>{msg.text}</div>}
      <button className="primary" onClick={saveSettings} disabled={busy}>Сохранить</button>
    </div>
  )
}