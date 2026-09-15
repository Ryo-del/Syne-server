import '../metric-window.css'

function Study() {
  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            Study
          </h1>

          <p className="metric-description">
            Connected students
          </p>
        </div>

        <strong className="metric-value">
          300
        </strong>
      </header>

      <div className="metric-chart">
        Study graph
      </div>
    </main>
  )
}

export default Study