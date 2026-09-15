import '../metric-window.css'

function Network() {
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
          ↓ 12 MB/s ↑ 4 MB/s
        </strong>
      </header>

      <div className="metric-chart">
        Network graph
      </div>
    </main>
  )
}

export default Network