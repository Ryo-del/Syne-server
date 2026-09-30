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
type NetworkPoint = {
  timestamp: string
  download: number
  upload: number
}

type NetworkResponse = {
  metric: string
  unit: string
  points: NetworkPoint[]
}

function Network() {
  const [data, setData] = useState<NetworkPoint[]>([])
  const [currentDownload, setCurrentDownload] = useState(0)
  const [currentUpload, setCurrentUpload] = useState(0)

  useEffect(() => {
    const loadNetwork = async () => {
      try {
        const base = await apiBase()
        const response = await fetch(`${base}/api/metrics/network`)

        if (!response.ok) {
          throw new Error('Failed to fetch network metrics')
        }

        const result: NetworkResponse = await response.json()

        setData(result.points)

        if (result.points.length > 0) {
          const current =
            result.points[result.points.length - 1]

          setCurrentDownload(current.download)
          setCurrentUpload(current.upload)
        }
      } catch (err) {
        console.error('Network metrics error:', err)
      }
    }

    loadNetwork()

    const interval = setInterval(loadNetwork, 1000)

    return () => clearInterval(interval)
  }, [])

  const chartData = data.map((point) => ({
    time: new Date(point.timestamp).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }),
    download: point.download,
    upload: point.upload,
  }))

  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            Network
          </h1>

          <p className="metric-description">
            Network traffic
          </p>
        </div>

        <strong className="metric-value">
          ↓ {currentDownload.toFixed(1)} MB/s ↑ {currentUpload.toFixed(1)} MB/s
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
              unit=" MB/s"
            />

            <Tooltip
              formatter={(value, name) => [
                `${Number(value).toFixed(1)} MB/s`,
                name === 'download' ? 'Download' : 'Upload',
              ]}
            />

            <Line
              type="monotone"
              dataKey="download"
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />

            <Line
              type="monotone"
              dataKey="upload"
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

export default Network