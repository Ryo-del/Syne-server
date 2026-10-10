
import './users.css'
import AddUserModal from './AddUserModal'
import { useEffect, useMemo, useRef, useState } from 'react'
type StudentStatus = 'online' | 'offline' | 'authorized' | 'pending'
type ApiUser = {
  ID: number
  Login: string
  FName: string
  SName: string
  Role: string
  Claimed: boolean
  ClaimCode: string
  CreatedAt: number
}

type Student = {
  id: string
  fname: string
  sname: string
  name: string
  role: 'Ученик' | 'Учитель'
  roleCode: 'student' | 'teacher'
  status: StudentStatus
  statusLabel: string

  avatar: string
  claimCode: string
}

type SortOption = 'name' | 'role' | 'status'

type ContextMenuState = {
  studentId: string
  x: number
  y: number
}
type ModalState =
  | { mode: 'create' }
  | { mode: 'edit'; student: Student }
  | null

function userToStudent(user: ApiUser): Student {
  const isTeacher = user.Role === 'teacher'

  return {
    id: user.Login,
    fname: user.FName,
    sname: user.SName,
    name: `${user.FName} ${user.SName}`,
    role: isTeacher ? 'Учитель' : 'Ученик',
    roleCode: isTeacher ? 'teacher' : 'student',
    status: user.Claimed ? 'authorized' : 'pending',
    statusLabel: user.Claimed ? 'Авторизован' : 'Ожидает активации',
    avatar: isTeacher ? '👩‍🏫' : '👨‍🎓',
    claimCode: user.ClaimCode ?? '',
  }
}

function Students() {
  const [students, setStudents] = useState<Student[]>([])
  const [search, setSearch] = useState('')
  const [sortBy, setSortBy] = useState<SortOption>('name')
  const [modalState, setModalState] = useState<ModalState>(null)
  const [contextMenu, setContextMenu] =
    useState<ContextMenuState | null>(null)

  const contextMenuRef = useRef<HTMLDivElement | null>(null)

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


      useEffect(() => {
    loadUsers()
    const timer = setInterval(loadUsers, 5000)
    return () => clearInterval(timer)
  }, [])
  useEffect(() => {
    if (!contextMenu) {
      return
    }
   function handleClickOutside(event: MouseEvent) {
      if (
        contextMenuRef.current &&
        !contextMenuRef.current.contains(event.target as Node)
      ) {
        setContextMenu(null)
      }
    }

    function handleEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setContextMenu(null)
      }
    }

    window.addEventListener('mousedown', handleClickOutside)
    window.addEventListener('keydown', handleEscape)

    return () => {
      window.removeEventListener('mousedown', handleClickOutside)
      window.removeEventListener('keydown', handleEscape)
    }
  }, [contextMenu])
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
      setModalState(null)
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

  const handleEditUser = async (user: {
    id: string
    role: 'Ученик' | 'Учитель'
    name: string
    surname: string
  }) => {
    try {
      const response = await fetch(
        `http://localhost:8080/api/users/${encodeURIComponent(user.id)}`,
        {
          method: 'PATCH',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            fname: user.name,
            sname: user.surname,
            role: user.role === 'Учитель' ? 'teacher' : 'student',
          }),
        }
      )

      if (!response.ok) {
        const data = await response.json().catch(() => null)

        throw new Error(
          data?.error ||
            `Не удалось обновить пользователя (${response.status})`
        )
      }

      await loadUsers()
      setModalState(null)
    } catch (error) {
      console.error('Ошибка редактирования пользователя:', error)

      if (error instanceof Error) {
        alert(error.message)
      } else {
        alert('Не удалось обновить пользователя')
      }
    }
  }

  const handleDeleteUser = async (studentId: string) => {
    const student = students.find((item) => item.id === studentId)
    const label = student ? student.name : studentId

    if (
      !window.confirm(
        `Удалить пользователя «${label}»? Это действие необратимо.`
      )
    ) {
      return
    }

    try {
      const response = await fetch(
        `http://localhost:8080/api/users/${encodeURIComponent(studentId)}`,
        { method: 'DELETE' }
      )

      if (!response.ok) {
        const data = await response.json().catch(() => null)

        throw new Error(
          data?.error ||
            `Не удалось удалить пользователя (${response.status})`
        )
      }

      await loadUsers()
    } catch (error) {
      console.error('Ошибка удаления пользователя:', error)

      if (error instanceof Error) {
        alert(error.message)
      } else {
        alert('Не удалось удалить пользователя')
      }
    } finally {
      setContextMenu(null)
    }
  }

  const handleMenuButtonClick = (
    event: React.MouseEvent,
    studentId: string
  ) => {
    event.stopPropagation()

    const rect = event.currentTarget.getBoundingClientRect()

    setContextMenu((current) =>
      current?.studentId === studentId
        ? null
        : {
            studentId,
            x: rect.right,
            y: rect.bottom + 6,
          }
    )
  }

  const contextMenuStudent = contextMenu
    ? students.find((item) => item.id === contextMenu.studentId) ?? null
    : null

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
            onClick={() => setModalState({ mode: 'create' })}
          >
            + Добавить пользователя
          </button>

        </div>
      </div>

      {modalState?.mode === 'create' && (
        <AddUserModal
          mode="create"
          onClose={() => setModalState(null)}
          onSubmit={handleAddUser}
        />
      )}

      {modalState?.mode === 'edit' && (
        <AddUserModal
          mode="edit"
          initialUser={{
            id: modalState.student.id,
            role: modalState.student.role,
            name: modalState.student.fname,
            surname: modalState.student.sname,
          }}
          onClose={() => setModalState(null)}
          onSubmit={handleEditUser}
        />
      )}

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
                  <span>{student.role}</span>

                  {student.claimCode && (
                    <span className="student-claim-code">
                      ClaimCode: {student.claimCode}
                    </span>
                  )}
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

            <button
              className="student-menu-button"
              onClick={(event) =>
                handleMenuButtonClick(event, student.id)
              }
            >
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

      {contextMenu && contextMenuStudent && (
        <div
          className="student-context-menu"
          ref={contextMenuRef}
          style={{ top: contextMenu.y, left: contextMenu.x }}
        >
          <button
            className="student-context-menu-item"
            onClick={() => {
              setModalState({ mode: 'edit', student: contextMenuStudent })
              setContextMenu(null)
            }}
          >
            ✎ Редактировать
          </button>

          <button
            className="student-context-menu-item danger"
            onClick={() => handleDeleteUser(contextMenuStudent.id)}
          >
            🗑 Удалить
          </button>
        </div>
      )}

    </div>
  )
}

export default Students