import './events.css'
import { apiBase } from '../../api'
import { useEffect, useState } from 'react'

type Report = { id: number; name: string; ip: string; error: string; created_at: number }
type ResponseData = { count: number; reports: Report[] }

export default function Events() {
  const [data, setData] = useState<ResponseData>({ count: 0, reports: [] })
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let active = true
    const load = async () => {
      try {
        const response = await fetch(`${await apiBase()}/api/error-reports`)
        if (!response.ok) throw new Error('Failed to load reports')
        const result: ResponseData = await response.json()
        if (active) { setData(result); setFailed(false) }
      } catch { if (active) setFailed(true) }
    }
    void load()
    const timer = setInterval(load, 5000)
    return () => { active = false; clearInterval(timer) }
  }, [])

  return <section className="events-page">
    <header className="events-header">
      <div><h1>Журнал событий</h1><p>Сообщения об ошибках, отправленные пользователями</p></div>
      <strong>{data.count}</strong>
    </header>
    {failed && <div className="events-empty">Не удалось загрузить журнал. Повторная попытка через несколько секунд.</div>}
    {!failed && !data.reports.length && <div className="events-empty">Отправленных ошибок пока нет</div>}
    <div className="events-list">
      {data.reports.map((report) => <article className="event-card" key={report.id}>
        <div className="event-meta"><strong>{report.name || 'Без имени'}</strong><span>{report.ip}</span><time>{new Date(report.created_at).toLocaleString()}</time></div>
        <div className="event-error">{report.error}</div>
      </article>)}
    </div>
  </section>
}
