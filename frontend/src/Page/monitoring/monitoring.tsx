import './monitoring.css'
import { openMetric } from '../../MetricWindow'
import { apiBase } from '../../api'
import { useEffect, useState } from 'react'

type Point = {
  timestamp: string
  value: number
}

type MetricResponse = {
  metric: string
  unit: string
  total?: number
  points: Point[]
}

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

type UptimeResponse = {
  seconds: number
}

function Monitoring() {
  const [cpu, setCPU] = useState(0)

  const [ram, setRAM] = useState(0)
  const [ramTotal, setRAMTotal] = useState(0)

  const [disk, setDisk] = useState(0)
  const [diskTotal, setDiskTotal] = useState(0)

  const [download, setDownload] = useState(0)
  const [upload, setUpload] = useState(0)

  const [uptime, setUptime] = useState(0)

  useEffect(() => {
    let cancelled = false

    const loadMetrics = async () => {
      try {
        const base = await apiBase()

        const [
          cpuResponse,
          ramResponse,
          diskResponse,
          networkResponse,
        ] = await Promise.all([
          fetch(`${base}/api/metrics/cpu`),
          fetch(`${base}/api/metrics/ram`),
          fetch(`${base}/api/metrics/disk`),
          fetch(`${base}/api/metrics/network`),
        ])

        if (
          !cpuResponse.ok ||
          !ramResponse.ok ||
          !diskResponse.ok ||
          !networkResponse.ok
        ) {
          throw new Error('Failed to fetch metrics')
        }

        const cpuData: MetricResponse = await cpuResponse.json()
        const ramData: MetricResponse = await ramResponse.json()
        const diskData: MetricResponse = await diskResponse.json()
        const networkData: NetworkResponse = await networkResponse.json()

        if (cancelled) return

        // CPU
        if (cpuData.points.length > 0) {
          setCPU(cpuData.points[cpuData.points.length - 1].value)
        }

        // RAM
        if (ramData.points.length > 0) {
          setRAM(ramData.points[ramData.points.length - 1].value)
        }

        if (ramData.total !== undefined) {
          setRAMTotal(ramData.total)
        }

        // Disk
        if (diskData.points.length > 0) {
          setDisk(diskData.points[diskData.points.length - 1].value)
        }

        if (diskData.total !== undefined) {
          setDiskTotal(diskData.total)
        }

        // Network
        if (networkData.points.length > 0) {
          const current = networkData.points[networkData.points.length - 1]

          setDownload(current.download)
          setUpload(current.upload)
        }
      } catch (err) {
        console.error('Monitoring metrics error:', err)
      }
    }

    const loadUptime = async () => {
      try {
        const base = await apiBase()
        const response = await fetch(`${base}/api/uptime`)

        if (!response.ok) {
          throw new Error('Failed to fetch uptime')
        }

        const data: UptimeResponse = await response.json()

        if (!cancelled) {
          setUptime(data.seconds)
        }
      } catch (err) {
        console.error('Uptime error:', err)
      }
    }

    loadMetrics()
    loadUptime()

    const metricsInterval = setInterval(loadMetrics, 1000)
    const uptimeInterval = setInterval(loadUptime, 1000)

    return () => {
      cancelled = true
      clearInterval(metricsInterval)
      clearInterval(uptimeInterval)
    }
  }, [])

  const diskPercent = diskTotal > 0 ? (disk / diskTotal) * 100 : 0

  const formatUptime = (seconds: number) => {
    const totalSeconds = Math.floor(seconds)

    const days = Math.floor(totalSeconds / 86400)
    const hours = Math.floor((totalSeconds % 86400) / 3600)
    const minutes = Math.floor((totalSeconds % 3600) / 60)

    return `${days}d ${hours}h ${minutes}m`
  }

  return (
    <>
      <header className="header">
        <div>
          <h1>Syne Server</h1>

          <div className="Uptime">
            <span className="status-dot" />
            Uptime: {formatUptime(uptime)}
          </div>
        </div>

        <div className="status">
          <span className="status-dot" />
          Online
        </div>
      </header>

      {/* System */}

      <div className="divider" />

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('CPU')}
        >
          CPU
        </button>

        <span className="param-value">{cpu.toFixed(1)}%</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('RAM')}
        >
          RAM
        </button>

        <span className="param-value">
          {ram.toFixed(1)} / {ramTotal.toFixed(1)} GB
        </span>
      </div>

      <div className="table-row">
        <span className="param-name">Disk</span>

        <span className="param-value">
          {disk.toFixed(1)} / {diskTotal.toFixed(1)} GB
        </span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Network')}
        >
          Network
        </button>

        <span className="param-value">
          ↓ {download.toFixed(1)} MB/s ↑ {upload.toFixed(1)} MB/s
        </span>
      </div>

      <div className="table-row">
        <span className="param-name">Syne Web</span>

        <span className="param-value status-online">online</span>
      </div>

      {/* Users */}

      <div className="divider" />

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Online Users')}
        >
          Online Users
        </button>

        <span className="param-value">1</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Connections')}
        >
          Connections
        </button>

        <span className="param-value">3</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Study')}
        >
          Study
        </button>

        <span className="param-value">2</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Teacher')}
        >
          Teacher
        </button>

        <span className="param-value">1</span>
      </div>

      {/* Storage */}

      <div className="divider" />

      <div className="table-row no-border">
        <span className="param-name section-title">Storage</span>
      </div>

      <div className="table-row no-border progress-row">
        <div className="progress-container">
          <div
            className="progress-bar"
            style={{ width: `${Math.min(diskPercent, 100)}%` }}
          />
        </div>

        <span className="param-value">{diskPercent.toFixed(0)}%</span>
      </div>

      <div className="table-row">
        <span className="param-name text-muted">
          Used: {disk.toFixed(1)} GB
        </span>

        <span className="param-value text-muted">
          Free: {Math.max(diskTotal - disk, 0).toFixed(1)} GB
        </span>
      </div>

      {/* Errors */}

      <div className="divider" />

      <div className="table-row no-border">
        <button
          className="param-name metric-button error-label"
          onClick={() => openMetric('Errors')}
        >
          Errors
        </button>

        <span className="param-value error-value">12</span>
      </div>
    </>
  )
}

export default Monitoring