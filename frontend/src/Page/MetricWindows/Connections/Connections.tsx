import '../metric-window.css'

function Connections() {
  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            Connections
          </h1>

          <p className="metric-description">
            Active connections
          </p>
        </div>

        <strong className="metric-value">
          412
        </strong>
      </header>

      <div className="metric-chart">
        Connections graph
      </div>
    </main>
  )
}

export default Connections