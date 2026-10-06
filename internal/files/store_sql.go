package files

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	protocol "github.com/Ryo-del/Syne-protocol"
)

var ErrQuotaExceeded = errors.New("quota exceeded")

const maxContacts = 10000

// Схема этапа 1: правила, контакты (для режима «только контактам») и учёт
// занятого места. Избранное и состояние UI добавятся на этапе 2.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS file_rules (
	owner  TEXT NOT NULL,
	path   TEXT NOT NULL,
	action TEXT NOT NULL,
	mode   TEXT NOT NULL,
	PRIMARY KEY (owner, path, action)
);

CREATE TABLE IF NOT EXISTS file_rule_users (
	owner   TEXT NOT NULL,
	path    TEXT NOT NULL,
	action  TEXT NOT NULL,
	grantee TEXT NOT NULL,
	PRIMARY KEY (owner, path, action, grantee)
);

CREATE TABLE IF NOT EXISTS user_contacts (
	owner   TEXT NOT NULL,
	contact TEXT NOT NULL,
	PRIMARY KEY (owner, contact)
);

CREATE TABLE IF NOT EXISTS file_usage (
	owner TEXT PRIMARY KEY,
	bytes INTEGER NOT NULL DEFAULT 0
);
`

// InitSchema создаёт таблицы (идемпотентно). Вызывать после db.InitSchema.
func InitSchema(db *sql.DB) error {
	_, err := db.Exec(schemaSQL + schemaExtraSQL)
	return err
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

var ruleTables = []string{"file_rules", "file_rule_users"}

type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

var _ Store = (*SQLStore)(nil)

// ---------- rules ----------

func (s *SQLStore) users(owner, path string, action protocol.Action) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT grantee FROM file_rule_users WHERE owner = ? AND path = ? AND action = ? ORDER BY grantee`,
		owner, path, string(action))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetRule(owner, path string, action protocol.Action) (Rule, bool, error) {
	var mode string
	err := s.db.QueryRow(
		`SELECT mode FROM file_rules WHERE owner = ? AND path = ? AND action = ?`,
		owner, path, string(action)).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, false, nil
	}
	if err != nil {
		return Rule{}, false, err
	}
	r := Rule{Mode: protocol.Mode(mode)}
	if r.Mode == protocol.ModeSelected {
		if r.Users, err = s.users(owner, path, action); err != nil {
			return Rule{}, false, err
		}
	}
	return r, true, nil
}

func (s *SQLStore) PutRule(owner, path string, action protocol.Action, r Rule) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO file_rules (owner, path, action, mode) VALUES (?, ?, ?, ?)
		 ON CONFLICT(owner, path, action) DO UPDATE SET mode = excluded.mode`,
		owner, path, string(action), string(r.Mode)); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM file_rule_users WHERE owner = ? AND path = ? AND action = ?`,
		owner, path, string(action)); err != nil {
		return err
	}
	if r.Mode == protocol.ModeSelected {
		for _, u := range r.Users {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO file_rule_users (owner, path, action, grantee) VALUES (?, ?, ?, ?)`,
				owner, path, string(action), u); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *SQLStore) DeleteRule(owner, path string, action protocol.Action) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tbl := range ruleTables {
		if _, err := tx.Exec(
			`DELETE FROM `+tbl+` WHERE owner = ? AND path = ? AND action = ?`,
			owner, path, string(action)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLStore) RulesUnder(owner, dir string, actions ...protocol.Action) ([]PathRule, error) {
	if len(actions) == 0 {
		return nil, nil
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	args := []any{owner, utf8.RuneCountInString(prefix), prefix}
	ph := make([]string, len(actions))
	for i, a := range actions {
		ph[i] = "?"
		args = append(args, string(a))
	}
	rows, err := s.db.Query(
		`SELECT path, action, mode FROM file_rules
		 WHERE owner = ? AND path <> '' AND substr(path, 1, ?) = ?
		   AND action IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	var out []PathRule
	for rows.Next() {
		var pr PathRule
		var action, mode string
		if err := rows.Scan(&pr.Path, &action, &mode); err != nil {
			rows.Close()
			return nil, err
		}
		pr.Action = protocol.Action(action)
		pr.Rule = Rule{Mode: protocol.Mode(mode)}
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // закрываем до вложенных запросов

	for i := range out {
		if out[i].Rule.Mode != protocol.ModeSelected {
			continue
		}
		if out[i].Rule.Users, err = s.users(owner, out[i].Path, out[i].Action); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// deletePrefix удаляет правила самого пути и всего его поддерева.
// Пустой путь — все правила владельца.
func deletePrefix(e execer, owner, path string) error {
	for _, tbl := range ruleTables {
		var err error
		if path == "" {
			_, err = e.Exec(`DELETE FROM `+tbl+` WHERE owner = ?`, owner)
		} else {
			_, err = e.Exec(
				`DELETE FROM `+tbl+` WHERE owner = ? AND (path = ? OR substr(path, 1, ?) = ?)`,
				owner, path, utf8.RuneCountInString(path)+1, path+"/")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// DeletePrefix — при удалении файла или папки.
func (s *SQLStore) DeletePrefix(owner, path string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deletePrefix(tx, owner, path); err != nil {
		return err
	}
	return tx.Commit()
}

// RenamePrefix переносит правила при переименовании или перемещении внутри
// одной папки владельца. Устаревшие правила в месте назначения удаляются.
func (s *SQLStore) RenamePrefix(owner, from, to string) error {
	if from == "" || to == "" {
		return errors.New("files: cannot rename the root")
	}
	if from == to {
		return nil
	}
	if strings.HasPrefix(to, from+"/") || strings.HasPrefix(from, to+"/") {
		return errors.New("files: invalid move between nested paths")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := deletePrefix(tx, owner, to); err != nil {
		return err
	}
	n := utf8.RuneCountInString(from)
	for _, tbl := range ruleTables {
		if _, err := tx.Exec(
			`UPDATE `+tbl+` SET path = ? || substr(path, ?)
			 WHERE owner = ? AND (path = ? OR substr(path, 1, ?) = ?)`,
			to, n+1, owner, from, n+1, from+"/"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- contacts ----------

func (s *SQLStore) IsContact(owner, contact string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM user_contacts WHERE owner = ? AND contact = ?`,
		owner, contact).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLStore) AddContact(owner, contact string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO user_contacts (owner, contact) VALUES (?, ?)`, owner, contact)
	return err
}

// ReplaceContacts полностью заменяет список контактов владельца
// (клиент присылает его целиком при добавлении или удалении контакта).
func (s *SQLStore) ReplaceContacts(owner string, contacts []string) error {
	if len(contacts) > maxContacts {
		return errors.New("files: too many contacts")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_contacts WHERE owner = ?`, owner); err != nil {
		return err
	}
	for _, c := range contacts {
		if c == "" || c == owner {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO user_contacts (owner, contact) VALUES (?, ?)`, owner, c); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------- usage / quota ----------

// Usage возвращает занятое место; known=false, если учёт ещё не заводился
// (тогда этап 2 пересчитывает его обходом папки).
func (s *SQLStore) Usage(owner string) (n int64, known bool, err error) {
	err = s.db.QueryRow(`SELECT bytes FROM file_usage WHERE owner = ?`, owner).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

func (s *SQLStore) SetUsage(owner string, n int64) error {
	_, err := s.db.Exec(
		`INSERT INTO file_usage (owner, bytes) VALUES (?, ?)
		 ON CONFLICT(owner) DO UPDATE SET bytes = excluded.bytes`, owner, n)
	return err
}

// ReserveUsage атомарно резервирует delta байт. limit < 0 — без лимита.
// Не помещается — ErrQuotaExceeded, учёт не меняется. Резервировать нужно до
// записи файла, а при неудаче записи вернуть место через ReleaseUsage.
func (s *SQLStore) ReserveUsage(owner string, delta, limit int64) error {
	if delta < 0 {
		return errors.New("files: negative reservation")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO file_usage (owner, bytes) VALUES (?, 0)`, owner); err != nil {
		return err
	}
	var res sql.Result
	if limit < 0 {
		res, err = tx.Exec(`UPDATE file_usage SET bytes = bytes + ? WHERE owner = ?`, delta, owner)
	} else {
		res, err = tx.Exec(
			`UPDATE file_usage SET bytes = bytes + ? WHERE owner = ? AND bytes + ? <= ?`,
			delta, owner, delta, limit)
	}
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrQuotaExceeded
	}
	return tx.Commit()
}

// ReleaseUsage возвращает delta байт (удаление файла, неудачная запись).
func (s *SQLStore) ReleaseUsage(owner string, delta int64) error {
	if delta < 0 {
		return errors.New("files: negative release")
	}
	_, err := s.db.Exec(
		`UPDATE file_usage SET bytes = MAX(0, bytes - ?) WHERE owner = ?`, delta, owner)
	return err
}

// ---------- user removal ----------

// PurgeUser стирает данные пользователя в таблицах файлового менеджера.
// Саму папку на диске удаляет Service.PurgeUser.
func (s *SQLStore) PurgeUser(login string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM file_rules WHERE owner = ?`, []any{login}},
		{`DELETE FROM file_rule_users WHERE owner = ? OR grantee = ?`, []any{login, login}},
		{`DELETE FROM user_contacts WHERE owner = ? OR contact = ?`, []any{login, login}},
		{`DELETE FROM file_usage WHERE owner = ?`, []any{login}},
		{`DELETE FROM file_favorites WHERE login = ? OR owner = ?`, []any{login, login}},
		{`DELETE FROM file_ui_state WHERE login = ?`, []any{login}},
		{`DELETE FROM user_blocked WHERE owner = ? OR blocked = ?`, []any{login, login}},
	}
	for _, st := range stmts {
		if _, err := tx.Exec(st.q, st.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ForgetUsage сбрасывает учёт места владельца: при следующей операции он
// пересчитается обходом папки.
func (s *SQLStore) ForgetUsage(owner string) error {
	_, err := s.db.Exec(`DELETE FROM file_usage WHERE owner = ?`, owner)
	return err
}
