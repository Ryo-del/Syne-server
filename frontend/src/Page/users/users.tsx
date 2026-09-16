
import './users.css'
import AddUserModal from './AddUserModal'
import { useEffect, useMemo, useState } from 'react'

type ApiUser = {
  ID: number
  Login: string
  FName: string
  SName: string
  Role: string
  Claimed: boolean
  CreatedAt: number
}
type Student = {
  id: string
  name: string
  role: 'Ученик' | 'Учитель'
  status: 'online' | 'offline'
  statusLabel: string
  avatar: string
}

type SortOption = 'name' | 'role' | 'status'

function userToStudent(user: ApiUser): Student {
  const isTeacher = user.Role === 'teacher'

  return {
    id: user.Login,
    name: `${user.FName} ${user.SName}`,
    role: isTeacher ? 'Учитель' : 'Ученик',
    status: 'offline',
    statusLabel: 'Не в сети',
    avatar: isTeacher ? '👩‍🏫' : '👨‍🎓',
  }
}

function Students() {
  const [students, setStudents] = useState<Student[]>([])
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<SortOption>('name')
  const [isAddModalOpen, setIsAddModalOpen] = useState(false)

  const loadUsers = async () => {
    try {
      const response = await fetch(
        'http://localhost:8080/api/users'
      )

      if (!response.ok) {
        throw new Error(
          `Backend returned ${response.status}`
        )
      }

      const users: ApiUser[] = await response.json()

      if (!Array.isArray(users)) {
        throw new Error('Backend returned invalid users data')
      }

      setStudents(users.map(userToStudent))
    } catch (error) {
      console.error('Ошибка загрузки пользователей:', error)
    }
  }

  useEffect(() => {
    loadUsers()
  }, [])

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
  }, [students, search, sortBy])

  const handleAddUser = async (user: {
  id: string
  role: 'Ученик' | 'Учитель'
  name: string
  surname: string
}) => {
  try {
    const body = new URLSearchParams()

    body.append('login', user.id)
    body.append('fname', user.name)
    body.append('sname', user.surname)

    body.append(
      'role',
      user.role === 'Учитель'
        ? 'teacher'
        : 'student'
    )

    const response = await fetch(
      'http://localhost:8080/api/users',
      {
        method: 'POST',
        headers: {
          'Content-Type':
            'application/x-www-form-urlencoded',
        },
        body: body.toString(),
      }
    )

    if (!response.ok) {
      const data = await response.json().catch(() => null)

      throw new Error(
        data?.error ||
          `Не удалось создать пользователя (${response.status})`
      )
    }

    await loadUsers()
    setIsAddModalOpen(false)
  } catch (error) {
    console.error(
      'Ошибка создания пользователя:',
      error
    )

    if (error instanceof Error) {
      alert(error.message)
    } else {
      alert('Не удалось создать пользователя')
    }
  }
}

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

          <button
            className="primary-button"
            onClick={() => setIsAddModalOpen(true)}
          >
            + Добавить пользователя
          </button>

          {isAddModalOpen && (
            <AddUserModal
              onClose={() => setIsAddModalOpen(false)}
              onAdd={handleAddUser}
            />
          )}

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
