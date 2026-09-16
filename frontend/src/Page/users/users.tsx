import './users.css'
import { useMemo, useState } from 'react'

type Student = {
  id: string
  name: string
  role: 'Ученик' | 'Учитель'
  status: 'online' | 'offline'
  statusLabel: string
  avatar: string
}

type SortOption = 'name' | 'role' | 'status'

const students: Student[] = [
  {
    id: '123456',
    name: 'Иван Иванов',
    role: 'Ученик',
    status: 'online',
    statusLabel: 'В сети',
    avatar: '👨‍🎓',
  },
  {
    id: '849201',
    name: 'Анна Петрова',
    role: 'Учитель',
    status: 'offline',
    statusLabel: 'Не в сети',
    avatar: '👩‍🏫',
  },
]

function Students() {
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<SortOption>('name')

  const filteredStudents = useMemo(() => {
    const query = search.toLowerCase().trim()

    const filtered = students.filter((student) => {
      return (
        student.name.toLowerCase().includes(query) ||
        student.id.includes(query) ||
        student.role.toLowerCase().includes(query)
      )
    })

    return [...filtered].sort((a, b) => {
      switch (sortBy) {
        case 'name':
          return a.name.localeCompare(b.name, 'ru')

        case 'role':
          return a.role.localeCompare(b.role, 'ru')

        case 'status':
          return (
            Number(b.status === 'online') -
            Number(a.status === 'online')
          )

        default:
          return 0
      }
    })
  }, [search, sortBy])

  return (
    <div className="students-page">

      <div className="students-header">
        <div>
          <h1>Пользователи</h1>
          <p>Ученики и преподаватели</p>
        </div>

        <div className="students-actions">
          <button className="secondary-button">
            Отсканировать список
          </button>

          <button className="secondary-button">
            Скачать список
          </button>

          <button className="primary-button">
            + Добавить пользователя
          </button>
        </div>
      </div>

      <div className="students-toolbar">

        <div className="students-search">
          <span>⌕</span>

          <input
            type="text"
            placeholder="Поиск пользователя..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>

        <select
          className="students-sort"
          value={sortBy}
          onChange={(e) =>
            setSortBy(e.target.value as SortOption)
          }
        >
          <option value="name">По имени</option>
          <option value="role">По роли</option>
          <option value="status">По статусу</option>
        </select>

      </div>

      <div className="students-list">

        {filteredStudents.map((student) => (
          <div
            className="student-row"
            key={student.id}
          >
            <div className="student-main">

              <div className="student-avatar">
                {student.avatar}
              </div>

              <div className="student-info">

                <div className="student-name">
                  {student.name}

                  <span className="student-id">
                    #{student.id}
                  </span>
                </div>

                <div className="student-role">
                  {student.role}
                </div>

              </div>
            </div>

            <div className="student-status">
              <span
                className={`status-dot ${student.status}`}
              />

              <span>
                {student.statusLabel}
              </span>
            </div>

            <button className="student-menu-button">
              ⋮
            </button>
          </div>
        ))}

        {filteredStudents.length === 0 && (
          <div className="students-empty">
            Пользователи не найдены
          </div>
        )}

      </div>

    </div>
  )
}

export default Students