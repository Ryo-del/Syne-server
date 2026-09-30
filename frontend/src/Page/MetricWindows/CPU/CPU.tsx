import '../metric-window.css'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts'
import { useEffect, useState } from 'react'
import { apiBase } from '../../../api'

type Point = {
  timestamp: string
  value: number
}

type CPUResponse = {
  metric: string
  unit: string
  points: Point[]
}

function CPU() {
  const [data, setData] = useState<Point[]>([])
  const [current, setCurrent] = useState(0)

  useEffect(() => {
    let cancelled = false

    const loadCPU = async () => {
      try {
        const base = await apiBase()
        const response = await fetch(`${base}/api/metrics/cpu`)

        if (!response.ok) {
          throw new Error('Failed to fetch CPU metrics')
        }

        const result: CPUResponse = await response.json()

        if (cancelled) return

        setData(result.points)

        if (result.points.length > 0) {
          setCurrent(result.points[result.points.length - 1].value)
        }
      } catch (err) {
        console.error('CPU metrics error:', err)
      }
    }

    loadCPU()

    const interval = setInterval(loadCPU, 1000)

    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [])

  const chartData = data.map((point) => ({
    time: new Date(point.timestamp).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }),
    value: point.value,
  }))

  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">CPU</h1>

          <p className="metric-description">Processor usage</p>
        </div>

        <strong className="metric-value">{current.toFixed(1)}%</strong>
      </header>

      <div className="metric-chart">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chartData}>
            <CartesianGrid strokeDasharray="3 3" />

            <XAxis dataKey="time" tick={{ fontSize: 11 }} />

            <YAxis domain={[0, 100]} tick={{ fontSize: 11 }} unit="%" />

            <Tooltip
              formatter={(value) => [`${Number(value).toFixed(1)}%`, 'CPU']}
            />

            <Line
              type="monotone"
              dataKey="value"
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </main>
  )
}

export default CPU