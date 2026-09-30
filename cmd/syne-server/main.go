package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"server/config"
	"server/internal/auth"
	control "server/internal/control"
	db "server/internal/db"
	directory "server/internal/directory"

	identity "server/internal/identity"
	metrics "server/internal/metrics"
	presence "server/internal/presence"
	vault "server/internal/vault"

	protocol "github.com/Ryo-del/Syne-protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

type Server struct {
	DB *sql.DB
}

// studyServerMDNSService — имя mDNS-сервиса, на котором study-сервер
// анонсирует себя, чтобы клиенты находили его автоматически, без ручного
// ввода адреса. ВАЖНО: должно дословно совпадать со строкой
// studyServerMDNSService в Syne/core/transport/p2p/discovery_server.go.
const studyServerMDNSService = "_syne-study-server._tcp"

// noopNotifee — серверу не нужно обнаруживать других через mDNS, только
// самому анонсироваться, поэтому обработчик найденных пиров пустой.
type noopNotifee struct{}

func (noopNotifee) HandlePeerFound(peer.AddrInfo) {}

// publicUser — то, что реально можно отдавать в панель лаборанта.
// В отличие от db.User здесь нет хэшей пароля/ключей.
type publicUser struct {
	ID        int64  `json:"ID"`
	Login     string `json:"Login"`
	FName     string `json:"FName"`
	SName     string `json:"SName"`
	Role      string `json:"Role"`
	Claimed   bool   `json:"Claimed"`
	ClaimCode string `json:"ClaimCode"`
	CreatedAt int64  `json:"CreatedAt"`
}

func toPublicUser(u db.User) publicUser {
	return publicUser{
		ID:        u.ID,
		Login:     u.Login,
		FName:     u.FName,
		SName:     u.SName,
		Role:      u.Role,
		Claimed:   u.Claimed,
		ClaimCode: u.ClaimCode,
		CreatedAt: u.CreatedAt,
	}
}

func ConnectToDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	return db, nil
}
func main() {
	var listenAddr, port, configPath string
	flag.StringVar(&configPath, "config", "", "path to config.json (default: user config dir)")
	flag.StringVar(&listenAddr, "listen", "", "override libp2p listen multiaddr")
	flag.StringVar(&port, "port", "", "override HTTP API port")
	flag.Parse()
	if configPath == "" {
		p, err := config.Path()
		if err != nil {
			slog.Error("can't resolve config path", "error", err)
			return
		}
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Error("server is not configured: complete first-run setup in the admin app", "config", configPath)
		} else {
			slog.Error("failed to load config", "error", err)
		}
		return
	}
	if listenAddr == "" {
		listenAddr = fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.P2PPort)
	}
	if port == "" {
		port = strconv.Itoa(cfg.HTTPPort)
	}
	if err := os.MkdirAll(cfg.FilesPath, 0o700); err != nil {
		slog.Error("can't create files dir", "error", err)
		return
	}
	slog.Info("config loaded", "name", cfg.Name, "http", port, "p2p", listenAddr, "files", cfg.FilesPath)

	startedAt := time.Now()
	path, err := os.Getwd()
	if err != nil {
		slog.Error("can't to get path", "error", err)
		return
	}
	path = filepath.Join(path, ".identity")

	privKey, err := identity.LoadOrCreateIdentity(path)
	if err != nil {
		slog.Error("Error with get privKey", "error", err)
		return
	}

	host, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(listenAddr),
	)
	if err != nil {
		slog.Error("error create host", "error", err)
		return
	}

	// Анонсируем себя через mDNS, чтобы клиенты в той же локальной сети
	// находили сервер автоматически, без ручного ввода multiaddr.
	// Работает только в пределах одного L2-сегмента (Wi-Fi точка доступа /
	// свитч) — через роутеры и интернет mDNS не проходит; для таких случаев
	// клиент по-прежнему поддерживает ручной адрес как запасной вариант.
	serverDiscovery := mdns.NewMdnsService(host, studyServerMDNSService, noopNotifee{})
	if err := serverDiscovery.Start(); err != nil {
		slog.Warn("failed to start LAN discovery advertisement", "error", err)
	} else {
		defer serverDiscovery.Close()
		slog.Info("advertising study server via mDNS", "service", studyServerMDNSService)
	}

	database, err := ConnectToDB("storage/syne.db")
	if err != nil {
		slog.Error("error to create database")
		return
	}
	err = db.InitSchema(database)
	if err != nil {
		slog.Error("failed to init schema", "error", err)
		return
	}
	controlhandler := control.NewControlHandler(host)
	addrs, err := fullAddrs(host)
	if err != nil {
		slog.Error("failed to resolve addrs", "error", err)
		return
	}
	collector := metrics.NewCollector(time.Duration(cfg.MonitorIntervalSec) * time.Second)
	store := config.NewStore(configPath, cfg)

	go HostApi(port, store, host, collector, startedAt, database)
	for _, a := range addrs {
		slog.Info("listening", "addr", a)
	}
	host.SetStreamHandler(protocol.SyncStreamProtocol, vault.NewHandler(database).HandleStream)
	host.SetStreamHandler(directory.ProtocolID, directory.NewHandler(database).HandleStream)
	host.SetStreamHandler(protocol.StreamProtocol, controlhandler.HandleStream)
	authHandler := auth.NewAuthHandler(database)
	host.SetStreamHandler(protocol.AuthStreamProtocol, authHandler.HandleStream)
	presenceHandler := presence.NewPresenceHandler()
	host.SetStreamHandler(protocol.PresenceStreamProtocol, presenceHandler.HandleStream)
	host.SetStreamHandler(protocol.SyncStreamProtocol, vault.NewHandler(database).HandleStream)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down")
	_ = host.Close()
	_ = database.Close()

}

var allowedOrigins = map[string]bool{
	"http://localhost:5173":   true, // vite dev
	"tauri://localhost":       true, // Tauri (macOS/Linux)
	"http://tauri.localhost":  true, // Tauri (Windows)
	"https://tauri.localhost": true,
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func HostApi(port string, store *config.Store, h host.Host, collector *metrics.Collector, startedAt time.Time, database *sql.DB) {
	mux := http.NewServeMux()
	metrics.RegisterRoutes(mux, collector)

	mux.HandleFunc("/api/uptime", func(w http.ResponseWriter, r *http.Request) {
		uptime := time.Since(startedAt)

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(map[string]float64{
			"seconds": uptime.Seconds(),
		})
	})
	mux.HandleFunc("/api/server-info", func(w http.ResponseWriter, r *http.Request) {
		cfg := store.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"name":       cfg.Name,
			"files_path": cfg.FilesPath,
		})
	})
	mux.HandleFunc("/api/settings", settingsHandler(store, collector))
	mux.HandleFunc("/api/client-config", func(w http.ResponseWriter, r *http.Request) {
		addrs, err := fullAddrs(h)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve addrs")
			return
		}
		name := strings.NewReplacer("\n", " ", "\r", " ").Replace(store.Get().Name)
		var b strings.Builder
		b.WriteString("# Syne client config\n")
		b.WriteString("name=" + name + "\n")
		b.WriteString("peer_id=" + h.ID().String() + "\n")
		for _, a := range addrs {
			// loopback и link-local клиенту не нужны
			if strings.HasPrefix(a, "/ip4/127.") || strings.HasPrefix(a, "/ip6/::1") || strings.HasPrefix(a, "/ip6/fe80") {
				continue
			}
			b.WriteString("addr=" + a + "\n")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	})
	mux.HandleFunc("/api/users/export", func(w http.ResponseWriter, r *http.Request) {
		exportUsersHandler(w, r, database)
	})
	mux.HandleFunc("/api/users/import", func(w http.ResponseWriter, r *http.Request) {
		importUsersHandler(w, r, database, store)
	})
	mux.HandleFunc("/api/users", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getUsersHandler(w, database)
		case http.MethodPost:
			createUserHandler(w, r, database, store)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
	mux.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		login := strings.TrimPrefix(r.URL.Path, "/api/users/")
		login = strings.Trim(login, "/")
		if login == "" {
			writeError(w, http.StatusBadRequest, "login is required")
			return
		}

		switch r.Method {
		case http.MethodPatch:
			updateUserHandler(w, r, database, login)
		case http.MethodDelete:
			deleteUserHandler(w, database, login)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	handler := cors(mux)

	slog.Info("HTTP API listening", "port", port)

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		slog.Error("HTTP API stopped", "error", err)
	}
}

func updateUserHandler(w http.ResponseWriter, r *http.Request, database *sql.DB, login string) {
	var req struct {
		FName string `json:"fname"`
		SName string `json:"sname"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	defer r.Body.Close()

	if strings.TrimSpace(req.FName) == "" || strings.TrimSpace(req.SName) == "" {
		writeError(w, http.StatusBadRequest, "fname and sname are required")
		return
	}

	if err := db.UpdateUser(database, login, req.FName, req.SName, req.Role); err != nil {
		status := http.StatusInternalServerError
		reason := "failed to update user"
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
			reason = "user not found"
		}
		writeError(w, status, reason)
		return
	}

	user, err := db.GetUserByLogin(database, login)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated user")
		return
	}

	writeJSON(w, http.StatusOK, toPublicUser(*user))
}

func deleteUserHandler(w http.ResponseWriter, database *sql.DB, login string) {
	if err := db.DeleteUser(database, login); err != nil {
		status := http.StatusInternalServerError
		reason := "failed to delete user"
		if errors.Is(err, db.ErrUserNotFound) {
			status = http.StatusNotFound
			reason = "user not found"
		}
		writeError(w, status, reason)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func getUsersHandler(w http.ResponseWriter, database *sql.DB) {
	users, err := db.GetAllUser(database)
	if err != nil {
		http.Error(w, "failed to get users", http.StatusInternalServerError)
		return
	}

	out := make([]publicUser, 0, len(users))
	for _, u := range users {
		out = append(out, toPublicUser(u))
	}

	writeJSON(w, http.StatusOK, out)
}
func fullAddrs(h host.Host) ([]string, error) {
	info := peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}
	addrs, err := peer.AddrInfoToP2pAddrs(&info)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out, nil

}
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}
