import '../metric-window.css'

function Teacher() {
  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            Teacher
          </h1>

          <p className="metric-description">
            Connected teachers
          </p>
        </div>

        <strong className="metric-value">
          47
        </strong>
      </header>

      <div className="metric-chart">
        Teacher graph
      </div>
    </main>
  )
}

export default Teacher