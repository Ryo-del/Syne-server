package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	Name      string `json:"name"`
	HTTPPort  int    `json:"http_port"`
	P2PPort   int    `json:"p2p_port"`
	FilesPath string `json:"files_path"`
	Autostart bool   `json:"autostart"`

	MonitorIntervalSec int `json:"monitor_interval_sec"`
	IDDigits           int `json:"id_digits"`
	ClaimCodeLength    int `json:"claim_code_length"`
}

func (c *Config) applyDefaults() {
	if c.MonitorIntervalSec == 0 {
		c.MonitorIntervalSec = 1
	}
	if c.IDDigits == 0 {
		c.IDDigits = 8
	}
	if c.ClaimCodeLength == 0 {
		c.ClaimCodeLength = 8
	}
}

func (c *Config) Validate() error {
	switch {
	case c.Name == "":
		return errors.New("name is empty")
	case c.HTTPPort < 1 || c.HTTPPort > 65535 || c.P2PPort < 1 || c.P2PPort > 65535:
		return errors.New("port must be 1..65535")
	case c.HTTPPort == c.P2PPort:
		return errors.New("HTTP and P2P ports must differ")
	case c.MonitorIntervalSec < 1 || c.MonitorIntervalSec > 60:
		return errors.New("monitor interval must be 1..60 seconds")
	case c.IDDigits < 4 || c.IDDigits > 12:
		return errors.New("id digits must be 4..12")
	case c.ClaimCodeLength < 6 || c.ClaimCodeLength > 32:
		return errors.New("claim code length must be 6..32")
	}
	return nil
}

// Path — общий путь с Tauri-частью (dirs::config_dir()/syne-server/config.json)
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "syne-server", "config.json"), nil
}

// Load возвращает os.ErrNotExist (обёрнутый), если первый запуск ещё не пройден.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	c.applyDefaults()
	return &c, nil
}

func Save(path string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Store — конфиг, который можно читать и менять во время работы.
type Store struct {
	mu sync.RWMutex

	path string
	cfg  Config
}

func NewStore(path string, c *Config) *Store {
	return &Store{path: path, cfg: *c}
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Update применяет fn к копии, валидирует, сохраняет на диск и только потом подменяет.
func (s *Store) Update(fn func(*Config)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg
	fn(&next)
	if err := next.Validate(); err != nil {
		return s.cfg, err
	}
	if err := Save(s.path, &next); err != nil {
		return s.cfg, err
	}
	s.cfg = next
	return next, nil
}
