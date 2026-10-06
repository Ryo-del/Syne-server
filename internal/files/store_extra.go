package files

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/Ryo-del/Syne-protocol"
)

const maxUIStateBytes = 16 * 1024

const schemaExtraSQL = `
CREATE TABLE IF NOT EXISTS file_favorites (
	login      TEXT NOT NULL,
	owner      TEXT NOT NULL,
	path_key   TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	PRIMARY KEY (login, owner, path_key)
);

CREATE TABLE IF NOT EXISTS file_ui_state (
	login      TEXT PRIMARY KEY,
	data       TEXT NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_blocked (
	owner   TEXT NOT NULL,
	blocked TEXT NOT NULL,
	PRIMARY KEY (owner, blocked)
);
`

// ---------- users (таблица users принадлежит пакету db) ----------

type User struct {
	Login string
	FName string
	SName string
	Role  string
}

// DisplayName — «Фамилия Имя»; если имени нет, логин.
func (u User) DisplayName() string {
	if n := strings.TrimSpace(u.SName + " " + u.FName); n != "" {
		return n
	}
	return u.Login
}

func (s *SQLStore) UserByLogin(login string) (User, bool, error) {
	var u User
	err := s.db.QueryRow(
		`SELECT login, fname, sname, role FROM users WHERE login = ?`, login,
	).Scan(&u.Login, &u.FName, &u.SName, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, err
	}
	return u, true, nil
}

func (s *SQLStore) AllUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT login, fname, sname, role FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Login, &u.FName, &u.SName, &u.Role); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// OwnersWithVisibleRules — владельцы, у которых есть хотя бы одно правило
// просмотра или вставки, отличное от «запретить всем». Кандидаты для списка
// «Сервер» (дальше каждого проверяет Engine.Visible).
func (s *SQLStore) OwnersWithVisibleRules() ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT owner FROM file_rules WHERE action IN ('view', 'paste') AND mode <> 'none'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---------- favorites ----------

type Favorite struct {
	Owner   string
	PathKey string // Fold(путь)
}

func (s *SQLStore) AddFavorite(login, owner, pathKey string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO file_favorites (login, owner, path_key, created_at) VALUES (?, ?, ?, ?)`,
		login, owner, pathKey, time.Now().UnixMilli())
	return err
}

func (s *SQLStore) RemoveFavorite(login, owner, pathKey string) error {
	_, err := s.db.Exec(
		`DELETE FROM file_favorites WHERE login = ? AND owner = ? AND path_key = ?`,
		login, owner, pathKey)
	return err
}

func (s *SQLStore) ListFavorites(login string) ([]Favorite, error) {
	rows, err := s.db.Query(
		`SELECT owner, path_key FROM file_favorites WHERE login = ? ORDER BY created_at, owner, path_key`,
		login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Favorite
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.Owner, &f.PathKey); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func deleteFavoritesPrefix(e execer, owner, path string) error {
	var err error
	if path == "" {
		_, err = e.Exec(`DELETE FROM file_favorites WHERE owner = ?`, owner)
	} else {
		_, err = e.Exec(
			`DELETE FROM file_favorites WHERE owner = ? AND (path_key = ? OR substr(path_key, 1, ?) = ?)`,
			owner, path, utf8.RuneCountInString(path)+1, path+"/")
	}
	return err
}

// DeleteFavoritesPrefix — при удалении элемента ссылки на него (у всех) пропадают.
func (s *SQLStore) DeleteFavoritesPrefix(owner, path string) error {
	return deleteFavoritesPrefix(s.db, owner, path)
}

// RenameFavoritesPrefix переносит ссылки (у всех пользователей) вслед за
// переименованным элементом. Пути приходят в форме Fold(...).
func (s *SQLStore) RenameFavoritesPrefix(owner, from, to string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteFavoritesPrefix(tx, owner, to); err != nil {
		return err
	}
	n := utf8.RuneCountInString(from)
	if _, err := tx.Exec(
		`UPDATE file_favorites SET path_key = ? || substr(path_key, ?)
		 WHERE owner = ? AND (path_key = ? OR substr(path_key, 1, ?) = ?)`,
		to, n+1, owner, from, n+1, from+"/"); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- UI state ----------

func (s *SQLStore) GetUIState(login string) (string, bool, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM file_ui_state WHERE login = ?`, login).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return data, true, nil
}

var errBadUIState = errors.New("files: ui state must be valid JSON up to 16 KiB")

func (s *SQLStore) PutUIState(login, data string) error {
	if len(data) > maxUIStateBytes || !json.Valid([]byte(data)) {
		return errBadUIState
	}
	_, err := s.db.Exec(
		`INSERT INTO file_ui_state (login, data, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(login) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		login, data, time.Now().UnixMilli())
	return err
}

// ---------- snapshot ----------

// snapshot — правила и контакты ОДНОГО владельца в памяти. Нужен, чтобы список
// из сотен элементов не превращался в тысячи запросов к SQLite. Только чтение.
type snapshot struct {
	owner    string
	rules    map[string]Rule
	list     []PathRule
	contacts map[string]bool
}

var errSnapshotRO = errors.New("files: snapshot is read-only")

func snapKey(path string, a protocol.Action) string { return path + "\x00" + string(a) }

func (s *SQLStore) Snapshot(owner string) (*snapshot, error) {
	snap := &snapshot{owner: owner, rules: map[string]Rule{}, contacts: map[string]bool{}}

	rows, err := s.db.Query(`SELECT path, action, mode FROM file_rules WHERE owner = ?`, owner)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var path, action, mode string
		if err := rows.Scan(&path, &action, &mode); err != nil {
			rows.Close()
			return nil, err
		}
		snap.rules[snapKey(path, protocol.Action(action))] = Rule{Mode: protocol.Mode(mode)}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	urows, err := s.db.Query(
		`SELECT path, action, grantee FROM file_rule_users WHERE owner = ? ORDER BY grantee`, owner)
	if err != nil {
		return nil, err
	}
	for urows.Next() {
		var path, action, grantee string
		if err := urows.Scan(&path, &action, &grantee); err != nil {
			urows.Close()
			return nil, err
		}
		k := snapKey(path, protocol.Action(action))
		if r, ok := snap.rules[k]; ok && r.Mode == protocol.ModeSelected {
			r.Users = append(r.Users, grantee)
			snap.rules[k] = r
		}
	}
	if err := urows.Err(); err != nil {
		urows.Close()
		return nil, err
	}
	urows.Close()

	crows, err := s.db.Query(`SELECT contact FROM user_contacts WHERE owner = ?`, owner)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var c string
		if err := crows.Scan(&c); err != nil {
			crows.Close()
			return nil, err
		}
		snap.contacts[c] = true
	}
	if err := crows.Err(); err != nil {
		crows.Close()
		return nil, err
	}
	crows.Close()

	for k, r := range snap.rules {
		parts := strings.SplitN(k, "\x00", 2)
		snap.list = append(snap.list, PathRule{Path: parts[0], Action: protocol.Action(parts[1]), Rule: r})
	}
	return snap, nil
}

func (c *snapshot) GetRule(owner, path string, a protocol.Action) (Rule, bool, error) {
	if owner != c.owner {
		return Rule{}, false, errors.New("files: snapshot owner mismatch")
	}
	r, ok := c.rules[snapKey(path, a)]
	return r, ok, nil
}

func (c *snapshot) PutRule(string, string, protocol.Action, Rule) error { return errSnapshotRO }

func (c *snapshot) DeleteRule(string, string, protocol.Action) error { return errSnapshotRO }

func (c *snapshot) RulesUnder(owner, dir string, actions ...protocol.Action) ([]PathRule, error) {
	if owner != c.owner {
		return nil, errors.New("files: snapshot owner mismatch")
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	var out []PathRule
	for _, pr := range c.list {
		if pr.Path == "" || !strings.HasPrefix(pr.Path, prefix) {
			continue
		}
		for _, a := range actions {
			if pr.Action == a {
				out = append(out, pr)
				break
			}
		}
	}
	return out, nil
}

func (c *snapshot) IsContact(owner, contact string) (bool, error) {
	if owner != c.owner {
		return false, errors.New("files: snapshot owner mismatch")
	}
	return c.contacts[contact], nil
}

var _ Store = (*snapshot)(nil)

// ---------- blocked ----------

// IsBlocked: owner заблокировал пользователя login.
func (s *SQLStore) IsBlocked(owner, login string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM user_blocked WHERE owner = ? AND blocked = ?`, owner, login).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReplaceBlocked полностью заменяет список заблокированных пользователей.
func (s *SQLStore) ReplaceBlocked(owner string, logins []string) error {
	if len(logins) > maxContacts {
		return errors.New("files: too many blocked users")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_blocked WHERE owner = ?`, owner); err != nil {
		return err
	}
	for _, l := range logins {
		if l == "" || l == owner {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO user_blocked (owner, blocked) VALUES (?, ?)`, owner, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}
