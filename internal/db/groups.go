package db

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const DefaultGroupEmoji = "👥"

var (
	ErrGroupNotFound = errors.New("group not found")
	ErrGroupExists   = errors.New("group already exists")
)

type Group struct {
	ID        int64
	Name      string
	Emoji     string
	CreatedAt int64
	Members   int // только ученики
}

type GroupMember struct {
	Login   string
	FName   string
	SName   string
	Claimed bool
}

type UserGroupRef struct {
	ID   int64
	Name string
}

// GroupWithKey — группа вместе с общим ключом чата (выдаётся участникам по sync-каналу).
type GroupWithKey struct {
	ID    int64
	Name  string
	Emoji string
	Key   []byte
}

type GroupMessageRow struct {
	ID   int64
	Data []byte
}

const groupCols = `g.id, g.name, g.emoji, g.created_at,
	(SELECT COUNT(*) FROM users u WHERE u.group_id = g.id AND u.role = 'student')`

func scanGroup(row interface{ Scan(...any) error }) (*Group, error) {
	var g Group
	if err := row.Scan(&g.ID, &g.Name, &g.Emoji, &g.CreatedAt, &g.Members); err != nil {
		return nil, err
	}
	return &g, nil
}

func initGroupsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS study_groups (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL,
			name_key   TEXT NOT NULL UNIQUE,
			emoji      TEXT NOT NULL DEFAULT '👥',
			chat_key   BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS group_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			group_id   INTEGER NOT NULL,
			from_login TEXT NOT NULL,
			msg_id     TEXT NOT NULL UNIQUE,
			data       BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_group_messages_group ON group_messages(group_id, id);
	`); err != nil {
		return err
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'group_id'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN group_id INTEGER`); err != nil {
			return err
		}
	}
	return nil
}

// CreateGroup: name уже проверен вызывающим (files.CleanName).
func CreateGroup(dbConn *sql.DB, name, emoji string) (*Group, error) {
	if emoji == "" {
		emoji = DefaultGroupEmoji
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	res, err := dbConn.Exec(`
		INSERT INTO study_groups (name, name_key, emoji, chat_key, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, name, strings.ToLower(name), emoji, key, time.Now().Unix())
	if err != nil {
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
			return nil, ErrGroupExists
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return GetGroup(dbConn, id)
}

func GetGroup(dbConn *sql.DB, id int64) (*Group, error) {
	g, err := scanGroup(dbConn.QueryRow(
		`SELECT `+groupCols+` FROM study_groups g WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	return g, err
}

func GroupByName(dbConn *sql.DB, name string) (*Group, error) {
	g, err := scanGroup(dbConn.QueryRow(
		`SELECT `+groupCols+` FROM study_groups g WHERE g.name_key = ?`,
		strings.ToLower(strings.TrimSpace(name))))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGroupNotFound
	}
	return g, err
}

func ListGroups(dbConn *sql.DB) ([]Group, error) {
	rows, err := dbConn.Query(`SELECT ` + groupCols + ` FROM study_groups g`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func SetGroupEmoji(dbConn *sql.DB, id int64, emoji string) error {
	res, err := dbConn.Exec(`UPDATE study_groups SET emoji = ? WHERE id = ?`, emoji, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// DeleteGroup: участники остаются без группы (их папки в «Сервере» снова в корне),
// сообщения группы удаляются.
func DeleteGroup(dbConn *sql.DB, id int64) error {
	tx, err := dbConn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET group_id = NULL WHERE group_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM group_messages WHERE group_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM study_groups WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrGroupNotFound
	}
	return tx.Commit()
}

// AddUsersToGroup добавляет только учеников без группы. Остальных пропускает (skipped).
func AddUsersToGroup(dbConn *sql.DB, groupID int64, logins []string) (added, skipped int, err error) {
	tx, err := dbConn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var one int
	if err := tx.QueryRow(`SELECT 1 FROM study_groups WHERE id = ?`, groupID).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrGroupNotFound
		}
		return 0, 0, err
	}
	for _, login := range logins {
		res, err := tx.Exec(`
			UPDATE users SET group_id = ?
			WHERE login = ? AND role = 'student' AND group_id IS NULL
		`, groupID, login)
		if err != nil {
			return 0, 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		} else {
			skipped++
		}
	}
	return added, skipped, tx.Commit()
}

func RemoveUsersFromGroup(dbConn *sql.DB, groupID int64, logins []string) (int, error) {
	tx, err := dbConn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	removed := 0
	for _, login := range logins {
		res, err := tx.Exec(
			`UPDATE users SET group_id = NULL WHERE login = ? AND group_id = ?`, login, groupID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			removed++
		}
	}
	return removed, tx.Commit()
}

func GroupMembers(dbConn *sql.DB, groupID int64) ([]GroupMember, error) {
	rows, err := dbConn.Query(
		`SELECT login, fname, sname, claimed FROM users WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.Login, &m.FName, &m.SName, &m.Claimed); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		a := strings.ToLower(out[i].SName + " " + out[i].FName)
		b := strings.ToLower(out[j].SName + " " + out[j].FName)
		return a < b
	})
	return out, nil
}

// UserGroupMap: login -> группа (для списка пользователей в админке).
func UserGroupMap(dbConn *sql.DB) (map[string]UserGroupRef, error) {
	rows, err := dbConn.Query(`
		SELECT u.login, g.id, g.name
		FROM users u JOIN study_groups g ON g.id = u.group_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]UserGroupRef{}
	for rows.Next() {
		var login string
		var ref UserGroupRef
		if err := rows.Scan(&login, &ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		out[login] = ref
	}
	return out, rows.Err()
}

const teacherCond = `EXISTS (SELECT 1 FROM users WHERE login = ? AND lower(trim(role)) = 'teacher')`

// GroupsForUser: группа ученика; преподавателю — все группы. С ключами чата.
func GroupsForUser(dbConn *sql.DB, login string) ([]GroupWithKey, error) {
	rows, err := dbConn.Query(`
		SELECT g.id, g.name, g.emoji, g.chat_key FROM study_groups g
		WHERE g.id = (SELECT group_id FROM users WHERE login = ?)
		   OR `+teacherCond, login, login)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupWithKey
	for rows.Next() {
		var g GroupWithKey
		if err := rows.Scan(&g.ID, &g.Name, &g.Emoji, &g.Key); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// CanAccessGroup: участник группы или преподаватель.
func CanAccessGroup(dbConn *sql.DB, login string, groupID int64) (bool, error) {
	var one int
	err := dbConn.QueryRow(`
		SELECT 1 FROM study_groups g
		WHERE g.id = ? AND (g.id = (SELECT group_id FROM users WHERE login = ?)
		                    OR `+teacherCond+`)`, groupID, login, login).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// PushGroupMessage идемпотентен по msg_id.
func PushGroupMessage(dbConn *sql.DB, groupID int64, fromLogin, msgID string, data []byte) error {
	_, err := dbConn.Exec(`
		INSERT OR IGNORE INTO group_messages (group_id, from_login, msg_id, data, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, groupID, fromLogin, msgID, data, time.Now().Unix())
	return err
}

func FetchGroupMessages(dbConn *sql.DB, groupID, afterID int64, limit int) ([]GroupMessageRow, error) {
	rows, err := dbConn.Query(`
		SELECT id, data FROM group_messages
		WHERE group_id = ? AND id > ? ORDER BY id ASC LIMIT ?
	`, groupID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMessageRow
	for rows.Next() {
		var r GroupMessageRow
		if err := rows.Scan(&r.ID, &r.Data); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
