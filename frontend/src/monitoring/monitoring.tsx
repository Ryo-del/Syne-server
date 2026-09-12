import './Monitoring.css'
import { useState } from 'react'

function Monitoring() {
  const [activeMetric, setActiveMetric] = useState<string | null>(null)

  const openMetric = (metric: string) => {
    setActiveMetric(metric)
  }

  const closeMetric = () => {
    setActiveMetric(null)
  }

  return (
    <>
      <header className="header">
        <div>
          <h1>Syne Server</h1>

          <div className="Uptime">
            <span className="status-dot" />
            Uptime: 14d 2h 32m
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

        <span className="param-value">24%</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('RAM')}
        >
          RAM
        </button>

        <span className="param-value">4.2 / 16 GB</span>
      </div>

      <div className="table-row">
        <span className="param-name">Disk</span>
        <span className="param-value">126 / 500 GB</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Network')}
        >
          Network
        </button>

        <span className="param-value">
          ↓ 12 MB/s ↑ 4 MB/s
        </span>
      </div>

      <div className="table-row">
        <span className="param-name">Syne Web</span>

        <span className="param-value status-online">
          online
        </span>
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

        <span className="param-value">347</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Connections')}
        >
          Connections
        </button>

        <span className="param-value">412</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Study')}
        >
          Study
        </button>

        <span className="param-value">300</span>
      </div>

      <div className="table-row">
        <button
          className="param-name metric-button"
          onClick={() => openMetric('Teacher')}
        >
          Teacher
        </button>

        <span className="param-value">47</span>
      </div>

      {/* Storage */}

      <div className="divider" />

      <div className="table-row no-border">
        <span className="param-name section-title">
          Storage
        </span>
      </div>

      <div className="table-row no-border progress-row">
        <div className="progress-container">
          <div
            className="progress-bar"
            style={{ width: '82%' }}
          />
        </div>

        <span className="param-value">82%</span>
      </div>

      <div className="table-row">
        <span className="param-name text-muted">
          Used: 410 GB
        </span>

        <span className="param-value text-muted">
          Free: 90 GB
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

        <span className="param-value error-value">
          12
        </span>
      </div>

      {/* Metric window */}

      {activeMetric && (
        <div
          className="modal-overlay"
          onClick={closeMetric}
        >
          <div
            className="modal"
            onClick={(event) => event.stopPropagation()}
          >
            <div className="modal-header">
              <h2>{activeMetric}</h2>

              <button
                className="modal-close"
                onClick={closeMetric}
              >
                ×
              </button>
            </div>

            <div className="chart">
              Тут будет график {activeMetric}
            </div>
          </div>
        </div>
      )}
    </>
  )
}

export default Monitoring