import './App.css'

function App() {
  return (
    <div className="app">
      <aside className="sidebar">
        <div className="logo">
          <div className="logo-mark">S</div>
          <span>Syne server</span>
        </div>

        <nav>
          <button className="nav-item">
            <span>📊</span>
            Мониторинг
          </button>

          <button className="nav-item">
            <span>🎓</span>
            Ученики
          </button>

          <button className="nav-item">
            <span>👥</span>
            Группы
          </button>

          <button className="nav-item">
            <span>🗓️</span>
            Расписание
          </button>

          <button className="nav-item">
            <span>📝</span>
            Тесты
          </button>

          <button className="nav-item">
            <span>📁</span>
            Файлы
          </button>

          <button className="nav-item">
            <span>🔔</span>
            Уведомления
          </button>

          <button className="nav-item">
            <span>🌐</span>
            Сеть
          </button>

          <button className="nav-item">
            <span>🗄️</span>
            База данных
          </button>

          <button className="nav-item">
            <span>📋</span>
            Журнал событий
          </button>
        </nav>

        <button className="nav-item settings">
          <span>⚙</span>
          Settings
        </button>
      </aside>

      <main className="main">
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

        {/* Единый блок мониторинга в виде таблицы по вашему плану */}
        <section className="monitoring-table">
          {/* Раздел 1: Системные ресурсы */}
            <div className="divider" />
          <div className="table-row">
            <span className="param-name"> CPU</span>
            <span className="param-value">24%</span>
          </div>

          <div className="table-row">
            <span className="param-name"> RAM</span>
            <span className="param-value">4.2 / 16 GB</span>
          </div>

          <div className="table-row">
            <span className="param-name">Disk</span>
            <span className="param-value">126 / 500 GB</span>
          </div>

          <div className="table-row">
            <span className="param-name">Network</span>
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

          {/* Раздел 2: Пользователи */}
            <div className="divider" />
          <div className="table-row">
            <span className="param-name"> Online Users</span>
            <span className="param-value">347</span>
          </div>
            

          <div className="table-row">
            <span className="param-name"> Connections</span>
            <span className="param-value">412</span>
          </div>

          <div className="table-row">
            <span className="param-name"> Study</span>
            <span className="param-value">300</span>
          </div>

          <div className="table-row">
            <span className="param-name"> Teacher</span>
            <span className="param-value">47</span>
          </div>

          {/* Раздел 3: Хранилище */}
            <div className="divider" />
          <div className="table-row no-border">
            <span className="param-name section-title">
              Storage
            </span>
          </div>

          {/* Прогресс-бар заштрихованной шкалы 82% */}

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

          {/* Раздел 4: Ошибки */}
            <div className="divider" />
          <div className="table-row no-border">
            <span className="param-name error-label">
              Errors :
            </span>

            <span className="param-value error-value">
              12
            </span>
          </div>
        </section>
      </main>
    </div>
  )
}

export default App