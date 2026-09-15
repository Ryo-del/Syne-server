import './App.css'
import { useState } from 'react'
import Monitoring from './Page/monitoring/monitoring'
import CPU from './Page/MetricWindows/CPU/CPU'
import RAM from './Page/MetricWindows/RAM/RAM'
import Network from './Page/MetricWindows/Network/Network'
import OnlineUsers from './Page/MetricWindows/Online Users/OnlineUsers'
import Connections from './Page/MetricWindows/Connections/Connections'
import Study from './Page/MetricWindows/Study/Study'
import Teacher from './Page/MetricWindows/Teacher/Teacher'
import Errors from './Page/MetricWindows/Errors/Errors'

function App() {
    const [activePage, setActivePage] = useState('monitoring')
     const path = window.location.pathname

  switch (path) {
    case '/metric/cpu':
      return <CPU />

    case '/metric/ram':
      return <RAM />

    case '/metric/network':
      return <Network />

    case '/metric/online-users':
      return <OnlineUsers />

    case '/metric/connections':
      return <Connections />

    case '/metric/study':
      return <Study />

    case '/metric/teacher':
      return <Teacher />

    case '/metric/errors':
      return <Errors />

  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="logo">
          <div className="logo-mark">S</div>
          <span>Syne server</span>
        </div>

        <nav>
          <button className={`nav-item ${activePage === 'monitoring' ? 'active' : ''}`}
            onClick={() => setActivePage('monitoring')}
          >
            <span>📊</span>
            
            Мониторинг
          </button>

          <button className={`nav-item ${activePage === 'users' ? 'active' : ''}`}
            onClick={() => setActivePage('users')}
          >
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
     {activePage === 'monitoring' && <Monitoring />}
        {activePage === 'users' && <h1>Users</h1>}
        {activePage === 'settings' && <h1>Settings</h1>}
      </main>
    </div>
  )
}

export default App