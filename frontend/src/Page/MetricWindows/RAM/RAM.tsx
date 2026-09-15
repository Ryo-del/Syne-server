import '../metric-window.css'

function RAM() {
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
          4.2 / 16 GB
        </strong>
      </header>

      <div className="metric-chart">
        RAM graph
      </div>
    </main>
  )
}

export default RAM