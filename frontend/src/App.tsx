import './App.css'
import { useEffect, useState } from 'react'
import { invoke } from '@tauri-apps/api/core'
import Monitoring from './Page/monitoring/monitoring'
import CPU from './Page/MetricWindows/CPU/CPU'
import RAM from './Page/MetricWindows/RAM/RAM'
import Network from './Page/MetricWindows/Network/Network'
import OnlineUsers from './Page/MetricWindows/Online Users/OnlineUsers'
import Connections from './Page/MetricWindows/Connections/Connections'
import Settings from './Page/settings/Settings'
import Study from './Page/MetricWindows/Study/Study'
import Teacher from './Page/MetricWindows/Teacher/Teacher'
import Errors from './Page/MetricWindows/Errors/Errors'
import User from './Page/users/users'
import SetupWindow from './Page/Setup/SetupWindow'

function App() {
  
  const [activePage, setActivePage] = useState('monitoring')
  // null = ещё проверяем, false = первый запуск, true = уже настроен
  const [configured, setConfigured] = useState<boolean | null>(null)

  useEffect(() => {
    invoke<boolean>('is_configured')
      .then(setConfigured)
      .catch((e) => {
        console.error('is_configured failed', e)
        setConfigured(false) // лучше показать настройку, чем пустой экран
      })
  }, [])

  const path = window.location.pathname

  // Окна метрик — отдельные окна, проверка конфига им не нужна
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

  if (configured === null) return null // можно поставить спиннер
  if (!configured) return <SetupWindow onDone={() => setConfigured(true)} />

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="logo">
          <div className="logo-mark">S</div>
          <span>Syne server</span>
        </div>

        <nav>
          <button
            className={`nav-item ${activePage === 'monitoring' ? 'active' : ''}`}
            onClick={() => setActivePage('monitoring')}
          >
            <span>📊</span>
            Мониторинг
          </button>
          <button
            className={`nav-item ${activePage === 'users' ? 'active' : ''}`}
            onClick={() => setActivePage('users')}
          >
            <span>🎓</span>
            Ученики
          </button>

          <button className="nav-item">
            <span>📁</span>
            Файлы
          </button>

          <button className="nav-item">
            <span>📣</span>
            Уведомления
          </button>

          <button className="nav-item">
            <span>🌐</span>
            Сеть
          </button>

          <button className="nav-item">
            <span>📋</span>
            Журнал событий
          </button>
        </nav>

        <button
          className={`nav-item settings ${activePage === 'settings' ? 'active' : ''}`}
          onClick={() => setActivePage('settings')}
        >
          <span>⚙</span>
          Settings
        </button>
      </aside>

      <main className="main">
        {activePage === 'monitoring' && <Monitoring />}
        {activePage === 'users' && <User />}
        {activePage === 'settings' && <Settings />}
        
      </main>
    </div>
  )
}

export default App