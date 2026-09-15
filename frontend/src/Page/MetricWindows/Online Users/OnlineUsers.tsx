import '../metric-window.css'

function OnlineUsers() {
  return (
    <main className="metric-window">
      <header className="metric-header">
        <div>
          <h1 className="metric-title">
            Online Users
          </h1>

          <p className="metric-description">
            Currently online
          </p>
        </div>

        <strong className="metric-value">
          347
        </strong>
      </header>

      <div className="metric-chart">
        Online users graph
      </div>
    </main>
  )
}

export default OnlineUsers