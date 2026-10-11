import { useState } from 'react'

const PRESETS = ['👥', '📚', '💻', '🔬', '🧪', '🎓', '🚀', '⚡', '🌟', '🎯', '🧠', '🎨', '🏆', '🔧', '📐', '🌍']
const FORBIDDEN = /[\\/:*?"<>|]/

type Props = {
  mode: 'create' | 'emoji'
  initialName?: string
  initialEmoji?: string
  onClose: () => void
  onSubmit: (v: { name: string; emoji: string }) => void | Promise<void>
}

export default function GroupModal({ mode, initialName = '', initialEmoji = '👥', onClose, onSubmit }: Props) {
  const [name, setName] = useState(initialName)
  const [emoji, setEmoji] = useState(initialEmoji)
  const isCreate = mode === 'create'

  const trimmed = name.trim()
  const nameError = FORBIDDEN.test(trimmed)
    ? 'Нельзя использовать символы \\ / : * ? " < > |'
    : trimmed.length > 40
      ? 'Максимум 40 символов'
      : ''
  const canSubmit = !!emoji.trim() && (!isCreate || (!!trimmed && !nameError))

  return (
    <div className="modal-overlay" onMouseDown={onClose}>
      <div className="add-user-modal" onMouseDown={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2>{isCreate ? 'Добавить группу' : `Эмодзи группы «${initialName}»`}</h2>
            <p>{isCreate ? 'Создание новой группы' : 'Иконка группы в списке чатов'}</p>
          </div>
          <button className="modal-close" onClick={onClose}>
            ×
          </button>
        </div>

        <div className="modal-body">
          {isCreate && (
            <div className="form-field">
              <label>Название</label>
              <input
                type="text"
                autoFocus
                placeholder="Например, П-24"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
              {nameError && <p className="form-field-hint group-error">{nameError}</p>}
            </div>
          )}

          <div className="form-field">
            <label>Эмодзи</label>
            <div className="emoji-grid">
              {PRESETS.map((e) => (
                <button
                  type="button"
                  key={e}
                  className={`emoji-cell${emoji === e ? ' selected' : ''}`}
                  onClick={() => setEmoji(e)}
                >
                  {e}
                </button>
              ))}
            </div>
            <input
              type="text"
              placeholder="Или вставьте свой эмодзи"
              value={emoji}
              maxLength={16}
              onChange={(e) => setEmoji(e.target.value)}
            />
          </div>
        </div>

        <div className="modal-footer">
          <button className="secondary-button" onClick={onClose}>
            Отмена
          </button>
          <button
            className="primary-button"
            disabled={!canSubmit}
            onClick={() => onSubmit({ name: trimmed, emoji: emoji.trim() })}
          >
            {isCreate ? 'Создать' : 'Сохранить'}
          </button>
        </div>
      </div>
    </div>
  )
}