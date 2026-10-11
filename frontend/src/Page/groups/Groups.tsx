import './groups.css'
import { useEffect, useMemo, useRef, useState } from 'react'
import type { MouseEvent as ReactMouseEvent } from 'react'
import GroupModal from './GroupModal'
import MembersModal from './MembersModal'
import {
  type Group,
  createGroup,
  deleteGroup,
  listGroups,
  plural,
  setGroupEmoji,
} from './groupsApi'

type MenuState = { groupId: number; x: number; y: number }
type ModalState =
  | { kind: 'create' }
  | { kind: 'emoji'; group: Group }
  | { kind: 'add'; group: Group }
  | { kind: 'remove'; group: Group }
  | null

function fail(error: unknown, fallback: string) {
  console.error(fallback, error)
  alert(error instanceof Error ? error.message : fallback)
}

function Groups() {
  const [groups, setGroups] = useState<Group[]>([])
  const [search, setSearch] = useState('')
  const [modal, setModal] = useState<ModalState>(null)
  const [menu, setMenu] = useState<MenuState | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)

  const load = async () => {
    try {
      setGroups(await listGroups())
    } catch (error) {
      console.error('Ошибка загрузки групп:', error)
    }
  }

  useEffect(() => {
    load()
    const timer = setInterval(load, 5000)
    return () => clearInterval(timer)
  }, [])

  useEffect(() => {
    if (!menu) return
    const onDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenu(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenu(null)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [menu])

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim()
    return groups.filter((g) => g.name.toLowerCase().includes(q))
  }, [groups, search])

  const menuGroup = menu ? (groups.find((g) => g.id === menu.groupId) ?? null) : null

  const openMenu = (event: ReactMouseEvent<HTMLButtonElement>, groupId: number) => {
    event.stopPropagation()
    const rect = event.currentTarget.getBoundingClientRect()
    setMenu((cur) =>
      cur?.groupId === groupId ? null : { groupId, x: rect.right, y: rect.bottom + 6 },
    )
  }

  const handleCreate = async (v: { name: string; emoji: string }) => {
    try {
      await createGroup(v.name, v.emoji)
      await load()
      setModal(null)
    } catch (error) {
      fail(error, 'Не удалось создать группу')
    }
  }

  const handleEmoji = async (group: Group, emoji: string) => {
    try {
      await setGroupEmoji(group.id, emoji)
      await load()
      setModal(null)
    } catch (error) {
      fail(error, 'Не удалось сменить эмодзи')
    }
  }

  const handleDelete = async (group: Group) => {
    setMenu(null)
    if (
      !window.confirm(
        `Удалить группу «${group.name}»? Ученики останутся без группы, чат группы пропадёт у всех.`,
      )
    ) {
      return
    }
    try {
      await deleteGroup(group.id)
      await load()
    } catch (error) {
      fail(error, 'Не удалось удалить группу')
    }
  }

  return (
    <div className="students-page">
      <div className="students-header">
        <div>
          <h1>Группы</h1>
          <p>Группы учеников и их чаты</p>
        </div>
        <div className="students-actions">
          <button className="primary-button" onClick={() => setModal({ kind: 'create' })}>
            + Добавить группу
          </button>
        </div>
      </div>

      <div className="students-toolbar">
        <div className="students-search">
          <span>⌕</span>
          <input
            type="text"
            placeholder="Поиск группы..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
      </div>

      <div className="students-list">
        {filtered.map((group) => (
          <div className="student-row" key={group.id}>
            <div className="student-main">
              <button
                type="button"
                className="student-avatar group-avatar-button"
                title="Сменить эмодзи"
                onClick={() => setModal({ kind: 'emoji', group })}
              >
                {group.emoji}
              </button>
              <div className="student-info">
                <div className="student-name">{group.name}</div>
                <div className="group-count">
                  {group.members} {plural(group.members, 'ученик', 'ученика', 'учеников')}
                </div>
              </div>
            </div>
            <button
              className="student-menu-button"
              onClick={(event) => openMenu(event, group.id)}
            >
              ⋮
            </button>
          </div>
        ))}

        {filtered.length === 0 && <div className="students-empty">Группы не найдены</div>}
      </div>

      {menu && menuGroup && (
        <div
          className="student-context-menu"
          ref={menuRef}
          style={{ top: menu.y, left: menu.x }}
        >
          <button
            className="student-context-menu-item"
            onClick={() => {
              setModal({ kind: 'add', group: menuGroup })
              setMenu(null)
            }}
          >
            ＋ Добавить ученика
          </button>
          <button
            className="student-context-menu-item"
            onClick={() => {
              setModal({ kind: 'remove', group: menuGroup })
              setMenu(null)
            }}
          >
            − Удалить ученика
          </button>
          <button
            className="student-context-menu-item danger"
            onClick={() => handleDelete(menuGroup)}
          >
            🗑 Удалить группу
          </button>
        </div>
      )}

      {modal?.kind === 'create' && (
        <GroupModal mode="create" onClose={() => setModal(null)} onSubmit={handleCreate} />
      )}
      {modal?.kind === 'emoji' && (
        <GroupModal
          mode="emoji"
          initialName={modal.group.name}
          initialEmoji={modal.group.emoji}
          onClose={() => setModal(null)}
          onSubmit={(v) => handleEmoji(modal.group, v.emoji)}
        />
      )}
      {(modal?.kind === 'add' || modal?.kind === 'remove') && (
        <MembersModal
          mode={modal.kind}
          group={modal.group}
          onClose={() => setModal(null)}
          onDone={() => {
            setModal(null)
            load()
          }}
        />
      )}
    </div>
  )
}

export default Groups