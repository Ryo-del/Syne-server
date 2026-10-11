import { useEffect, useMemo, useState } from 'react'
import { type Group, addMembers, listMembers, listUsers, removeMembers } from './groupsApi'

type Row = { login: string; name: string; note?: string }

type Props = {
  mode: 'add' | 'remove'
  group: Group
  onClose: () => void
  onDone: () => void
}

export default function MembersModal({ mode, group, onClose, onDone }: Props) {
  const [rows, setRows] = useState<Row[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const isAdd = mode === 'add'

  useEffect(() => {
    let off = false
    async function load() {
      try {
        let next: Row[]
        if (isAdd) {
          const users = await listUsers()
          next = users
            .filter((u) => u.Role === 'student')
            .map((u) => ({
              login: u.Login,
              name: `${u.FName} ${u.SName}`,
              note:
                u.GroupID === 0
                  ? undefined
                  : u.GroupID === group.id
                    ? 'уже состоит в этой группе'
                    : `уже состоит в группе «${u.Group}»`,
            }))
        } else {
          const members = await listMembers(group.id)
          next = members.map((m) => ({ login: m.login, name: `${m.fname} ${m.sname}` }))
        }
        if (!off) setRows(next)
      } catch (error) {
        console.error(error)
        alert(error instanceof Error ? error.message : 'Не удалось загрузить список')
      } finally {
        if (!off) setLoading(false)
      }
    }
    load()
    return () => {
      off = true
    }
  }, [isAdd, group.id])

  // сначала свободные, потом уже состоящие в группах
  const visible = useMemo(() => {
    const q = search.toLowerCase().trim()
    return rows
      .filter((r) => r.name.toLowerCase().includes(q) || r.login.includes(q))
      .sort((a, b) => {
        if (!!a.note !== !!b.note) return a.note ? 1 : -1
        return a.name.localeCompare(b.name, 'ru')
      })
  }, [rows, search])

  const toggle = (login: string) =>
    setSelected((cur) => {
      const next = new Set(cur)
      if (next.has(login)) next.delete(login)
      else next.add(login)
      return next
    })

  const submit = async () => {
    if (selected.size === 0 || busy) return
    setBusy(true)
    try {
      const logins = [...selected]
      if (isAdd) await addMembers(group.id, logins)
      else await removeMembers(group.id, logins)
      onDone()
    } catch (error) {
      console.error(error)
      alert(error instanceof Error ? error.message : 'Не удалось выполнить операцию')
      setBusy(false)
    }
  }

  return (
    <div className="modal-overlay" onMouseDown={onClose}>
      <div className="add-user-modal" onMouseDown={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2>{isAdd ? 'Добавить учеников' : 'Удалить учеников'}</h2>
            <p>
              {group.emoji} {group.name}
            </p>
          </div>
          <button className="modal-close" onClick={onClose}>
            ×
          </button>
        </div>

        <div className="modal-body">
          <div className="students-search">
            <span>⌕</span>
            <input
              type="text"
              placeholder="Поиск ученика..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>

          <div className="members-list">
            {loading && <div className="students-empty">Загрузка…</div>}
            {!loading && visible.length === 0 && (
              <div className="students-empty">
                {isAdd ? 'Ученики не найдены' : 'В группе нет учеников'}
              </div>
            )}
            {visible.map((r) => {
              const disabled = !!r.note
              return (
                <label key={r.login} className={`check-row${disabled ? ' disabled' : ''}`}>
                  <input
                    type="checkbox"
                    disabled={disabled}
                    checked={selected.has(r.login)}
                    onChange={() => toggle(r.login)}
                  />
                  <div>
                    <div className="check-name">{r.name}</div>
                    {r.note && <div className="check-note">{r.note}</div>}
                  </div>
                </label>
              )
            })}
          </div>
        </div>

        <div className="modal-footer">
          <button className="secondary-button" onClick={onClose}>
            Отмена
          </button>
          <button
            className={`primary-button${isAdd ? '' : ' danger'}`}
            disabled={selected.size === 0 || busy}
            onClick={submit}
          >
            {isAdd ? 'Добавить' : 'Удалить'}
            {selected.size > 0 ? ` (${selected.size})` : ''}
          </button>
        </div>
      </div>
    </div>
  )
}