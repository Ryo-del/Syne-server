package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	db "server/internal/db"
	"server/internal/files"
)

type publicGroup struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Emoji   string `json:"emoji"`
	Members int    `json:"members"`
}

func toPublicGroup(g db.Group) publicGroup {
	return publicGroup{ID: g.ID, Name: g.Name, Emoji: g.Emoji, Members: g.Members}
}

// Правила как для имён в Windows (без \ / : * ? " < > |), "-" разрешён.
// Название становится частью пути в «Сервере», поэтому проверяем как имя папки.
func validGroupName(raw string) (string, error) {
	name, err := files.CleanName(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("недопустимое название: нельзя символы \\ / : * ? \" < > |")
	}
	if utf8.RuneCountInString(name) > 40 {
		return "", errors.New("название слишком длинное (максимум 40 символов)")
	}
	return name, nil
}

func validEmoji(raw string) (string, error) {
	e := strings.TrimSpace(raw)
	if e == "" {
		return db.DefaultGroupEmoji, nil
	}
	if len(e) > 32 {
		return "", errors.New("недопустимый эмодзи")
	}
	return e, nil
}

func groupFailure(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, db.ErrGroupNotFound):
		writeError(w, http.StatusNotFound, "группа не найдена")
	case errors.Is(err, db.ErrGroupExists):
		writeError(w, http.StatusConflict, "группа с таким названием уже существует")
	default:
		slog.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, what)
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// /api/groups
func groupsHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			groups, err := db.ListGroups(database)
			if err != nil {
				groupFailure(w, err, "failed to list groups")
				return
			}
			out := make([]publicGroup, 0, len(groups))
			for _, g := range groups {
				out = append(out, toPublicGroup(g))
			}
			writeJSON(w, http.StatusOK, out)
		case http.MethodPost:
			var req struct {
				Name  string `json:"name"`
				Emoji string `json:"emoji"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			name, err := validGroupName(req.Name)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			emoji, err := validEmoji(req.Emoji)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			g, err := db.CreateGroup(database, name, emoji)
			if err != nil {
				groupFailure(w, err, "failed to create group")
				return
			}
			writeJSON(w, http.StatusOK, toPublicGroup(*g))
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// /api/groups/{id}  и  /api/groups/{id}/members
func groupRoutes(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/groups/"), "/")
		idStr, sub, _ := strings.Cut(rest, "/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid group id")
			return
		}

		switch {
		case sub == "" && r.Method == http.MethodPatch:
			var req struct {
				Emoji string `json:"emoji"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			emoji, err := validEmoji(req.Emoji)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := db.SetGroupEmoji(database, id, emoji); err != nil {
				groupFailure(w, err, "failed to update group")
				return
			}
			g, err := db.GetGroup(database, id)
			if err != nil {
				groupFailure(w, err, "failed to load group")
				return
			}
			writeJSON(w, http.StatusOK, toPublicGroup(*g))

		case sub == "" && r.Method == http.MethodDelete:
			if err := db.DeleteGroup(database, id); err != nil {
				groupFailure(w, err, "failed to delete group")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})

		case sub == "members" && r.Method == http.MethodGet:
			if _, err := db.GetGroup(database, id); err != nil {
				groupFailure(w, err, "failed to load group")
				return
			}
			members, err := db.GroupMembers(database, id)
			if err != nil {
				groupFailure(w, err, "failed to list members")
				return
			}
			out := make([]map[string]any, 0, len(members))
			for _, m := range members {
				out = append(out, map[string]any{
					"login": m.Login, "fname": m.FName, "sname": m.SName, "claimed": m.Claimed,
				})
			}
			writeJSON(w, http.StatusOK, out)

		case sub == "members" && (r.Method == http.MethodPost || r.Method == http.MethodDelete):
			var req struct {
				Logins []string `json:"logins"`
			}
			if !decodeBody(w, r, &req) {
				return
			}
			if len(req.Logins) == 0 || len(req.Logins) > 1000 {
				writeError(w, http.StatusBadRequest, "logins: от 1 до 1000 элементов")
				return
			}
			if r.Method == http.MethodPost {
				added, skipped, err := db.AddUsersToGroup(database, id, req.Logins)
				if err != nil {
					groupFailure(w, err, "failed to add members")
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"added": added, "skipped": skipped})
				return
			}
			if _, err := db.GetGroup(database, id); err != nil {
				groupFailure(w, err, "failed to load group")
				return
			}
			removed, err := db.RemoveUsersFromGroup(database, id, req.Logins)
			if err != nil {
				groupFailure(w, err, "failed to remove members")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"removed": removed})

		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
