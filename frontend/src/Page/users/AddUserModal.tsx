
import './add-user-modal.css'
import type { Group } from '../groups/groupsApi'
import { useState } from 'react'

type UserFormValues = {
  id: string
  role: 'Ученик' | 'Учитель'
  name: string
  surname: string
  groupId: string // '' — без группы
}

type AddUserModalProps = {
  mode: 'create' | 'edit'
  initialUser?: UserFormValues
  groups?: Group[]
  onClose: () => void
  onSubmit: (user: UserFormValues) => void
}

function generateId() {
  return Math.floor(
    100000 + Math.random() * 900000
  ).toString()
}

function AddUserModal({
  mode,
  initialUser,
  groups = [],
  onClose,
  onSubmit,
}: AddUserModalProps) {
  const isEdit = mode === 'edit'

  const [id, setId] = useState(
    initialUser?.id ?? generateId()
  )

  const [role, setRole] = useState<'Ученик' | 'Учитель'>(
    initialUser?.role ?? 'Ученик'
  )
    const [groupId, setGroupId] = useState(initialUser?.groupId ?? '')
  const [name, setName] = useState(initialUser?.name ?? '')

  const [surname, setSurname] = useState(
    initialUser?.surname ?? ''
  )

  const handleSubmit = () => {
    if (!name.trim() || !surname.trim()) {
      return
    }

    onSubmit({
      id,
      role,
      name: name.trim(),
      surname: surname.trim(),
      groupId: groupId,
    })
  }

  return (
    <div
      className="modal-overlay"
      onMouseDown={onClose}
    >

      <div
        className="add-user-modal"
        onMouseDown={(e) =>
          e.stopPropagation()
        }
      >

        <div className="modal-header">

          <div>
            <h2>
              {isEdit
                ? 'Редактировать пользователя'
                : 'Добавить пользователя'}
            </h2>
            <p>
              {isEdit
                ? 'Изменение данных пользователя'
                : 'Создание нового пользователя'}
            </p>
          </div>

          <button
            className="modal-close"
            onClick={onClose}
          >
            ×
          </button>

        </div>

        <div className="modal-body">

          <div className="form-field">

            <label>ID</label>

            <div className="id-input-wrapper">

              <input
                type="text"
                value={id}
                maxLength={6}
                disabled={isEdit}
                onChange={(e) =>
                  setId(
                    e.target.value
                      .replace(/\D/g, '')
                      .slice(0, 6)
                  )
                }
              />

              {!isEdit && (
                <button
                  className="generate-id-button"
                  title="Сгенерировать ID"
                  onClick={() =>
                    setId(generateId())
                  }
                >
                  🎲
                </button>
              )}

            </div>

            {isEdit && (
              <p className="form-field-hint">
                Логин нельзя изменить после создания
              </p>
            )}

          </div>

          <div className="form-field">

            <label>Статус</label>

            <div className="role-options">

              <label className="radio-option">

                <input
                  type="radio"
                  name="role"
                  value="Ученик"
                  checked={role === 'Ученик'}
                  onChange={() =>
                    setRole('Ученик')
                  }
                />

                <span>Ученик</span>

              </label>

              <label className="radio-option">

                <input
                  type="radio"
                  name="role"
                  value="Учитель"
                  checked={role === 'Учитель'}
                  onChange={() =>
                    setRole('Учитель')
                  }
                />

                <span>Учитель</span>

              </label>

            </div>

          </div>
                            {!isEdit && role === 'Ученик' && (
            <div className="form-field">
              <label>Группа (необязательно)</label>
              <select value={groupId} onChange={(e) => setGroupId(e.target.value)}>
                <option value="">Без группы</option>
                {groups.map((g) => (
                  <option key={g.id} value={String(g.id)}>
                    {g.emoji} {g.name}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="form-field">

            <label>Имя</label>

            <input
              type="text"
              placeholder="Введите имя"
              value={name}
              onChange={(e) =>
                setName(e.target.value)
              }
            />

          </div>

          <div className="form-field">

            <label>Фамилия</label>

            <input
              type="text"
              placeholder="Введите фамилию"
              value={surname}
              onChange={(e) =>
                setSurname(e.target.value)
              }
            />

          </div>

        </div>

        <div className="modal-footer">

          <button
            className="secondary-button"
            onClick={onClose}
          >
            Отмена
          </button>

          <button
            className="primary-button"
            disabled={
              !name.trim() ||
              !surname.trim()
            }
            onClick={handleSubmit}
          >
            {isEdit ? 'Сохранить' : 'Добавить'}
          </button>

        </div>

      </div>

    </div>
  )
}

export default AddUserModal