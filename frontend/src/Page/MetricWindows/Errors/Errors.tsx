import '../metric-window.css'
import { apiBase } from '../../../api'
import { useEffect, useState } from 'react'

type ErrorReport = { id: number; name: string; ip: string; error: string; created_at: number }
type ErrorResponse = { count: number; reports: ErrorReport[] }

function Errors() {
  const [data, setData] = useState<ErrorResponse>({ count: 0, reports: [] })

  useEffect(() => {
    let active = true
    const load = async () => {
      try {
        const response = await fetch(`${await apiBase()}/api/error-reports`)
        if (!response.ok) throw new Error('Failed to fetch error reports')
        const result: ErrorResponse = await response.json()
        if (active) setData(result)
      } catch (error) { console.error('Error reports error:', error) }
    }
    void load()
    const timer = setInterval(load, 5000)
    return () => { active = false; clearInterval(timer) }
  }, [])

  return (
    <main className="metric-window error-reports-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">Ошибки</h1>
          <p className="metric-description">Сообщения пользователей</p>
        </div>
        <strong className="metric-value error">{data.count}</strong>
      </header>
      <div className="error-report-list">
        {data.reports.map((report) => (
          <article className="error-report-card" key={report.id}>
            <div className="error-report-meta">
              <strong>{report.name || 'Без имени'}</strong>
              <span>{report.ip}</span>
              <time>{new Date(report.created_at).toLocaleString()}</time>
            </div>
            <p>{report.error}</p>
          </article>
        ))}
        {!data.reports.length && <div className="error-report-empty">Отправленных ошибок пока нет</div>}
      </div>
    </main>
  )
}

export default Errors
