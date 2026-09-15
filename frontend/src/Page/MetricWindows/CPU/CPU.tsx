import '../metric-window.css'

function CPU() {
  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            CPU
          </h1>

          <p className="metric-description">
            Processor usage
          </p>
        </div>

        <strong className="metric-value">
          24%
        </strong>
      </header>

      <div className="metric-chart">
        CPU graph
      </div>
    </main>
  )
}

export default CPU