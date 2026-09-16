
import './add-user-modal.css'
import { useState } from 'react'

type AddUserModalProps = {
  onClose: () => void

  onAdd: (user: {
    id: string
    role: 'Ученик' | 'Учитель'
    name: string
    surname: string
  }) => void
}

function generateId() {
  return Math.floor(
    100000 + Math.random() * 900000
  ).toString()
}

function AddUserModal({
  onClose,
  onAdd,
}: AddUserModalProps) {

  const [id, setId] = useState(generateId())

  const [role, setRole] =
    useState<'Ученик' | 'Учитель'>('Ученик')

  const [name, setName] = useState('')

  const [surname, setSurname] = useState('')

  const handleSubmit = () => {

    if (
      !name.trim() ||
      !surname.trim()
    ) {
      return
    }

    onAdd({
      id,
      role,
      name: name.trim(),
      surname: surname.trim(),
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
            <h2>Добавить пользователя</h2>
            <p>Создание нового пользователя</p>
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
                onChange={(e) =>
                  setId(
                    e.target.value
                      .replace(/\D/g, '')
                      .slice(0, 6)
                  )
                }
              />

              <button
                className="generate-id-button"
                title="Сгенерировать ID"
                onClick={() =>
                  setId(generateId())
                }
              >
                🎲
              </button>

            </div>

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
            Добавить
          </button>

        </div>

      </div>

    </div>
  )
}

export default AddUserModal
