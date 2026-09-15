import '../metric-window.css'

function Errors() {
  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            Errors
          </h1>

          <p className="metric-description">
            Server errors
          </p>
        </div>

        <strong className="metric-value error">
          12
        </strong>
      </header>

      <div className="metric-chart">
        Errors graph
      </div>
    </main>
  )
}

export default Errors