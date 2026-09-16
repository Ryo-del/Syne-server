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

type Point = {
  timestamp: string
  value: number
}

type RAMResponse = {
  metric: string
  unit: string
  total: number
  points: Point[]
}

function RAM() {
  const [data, setData] = useState<Point[]>([])
  const [current, setCurrent] = useState(0)
  const [total, setTotal] = useState(0)

  useEffect(() => {
    const loadRAM = async () => {
      try {
        const response = await fetch(
          'http://localhost:8080/api/metrics/ram'
        )

        if (!response.ok) {
          throw new Error('Failed to fetch RAM metrics')
        }

        const result: RAMResponse = await response.json()

        setData(result.points)
        setTotal(result.total)

        if (result.points.length > 0) {
          setCurrent(
            result.points[result.points.length - 1].value
          )
        }
      } catch (err) {
        console.error('RAM metrics error:', err)
      }
    }

    loadRAM()

    const interval = setInterval(loadRAM, 1000)

    return () => clearInterval(interval)
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
          <h1 className="metric-title">
            RAM
          </h1>

          <p className="metric-description">
            Memory usage
          </p>
        </div>

        <strong className="metric-value">
          {current.toFixed(1)} / {total.toFixed(1)} GB
        </strong>
      </header>

      <div className="metric-chart">
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chartData}>
            <CartesianGrid strokeDasharray="3 3" />

            <XAxis
              dataKey="time"
              tick={{ fontSize: 11 }}
            />

            <YAxis
              tick={{ fontSize: 11 }}
              unit=" GB"
            />

            <Tooltip
              formatter={(value) => [
                `${Number(value).toFixed(1)} GB`,
                'RAM',
              ]}
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

export default RAM