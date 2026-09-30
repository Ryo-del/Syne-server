package main

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"

	"server/config"
	metrics "server/internal/metrics"
)

type settingsView struct {
	Name               string `json:"name"`
	HTTPPort           int    `json:"http_port"`
	P2PPort            int    `json:"p2p_port"`
	MonitorIntervalSec int    `json:"monitor_interval_sec"`
	IDDigits           int    `json:"id_digits"`
	ClaimCodeLength    int    `json:"claim_code_length"`
}

func viewOf(c config.Config) settingsView {
	return settingsView{c.Name, c.HTTPPort, c.P2PPort, c.MonitorIntervalSec, c.IDDigits, c.ClaimCodeLength}
}

type settingsPatch struct {
	HTTPPort           *int `json:"http_port"`
	P2PPort            *int `json:"p2p_port"`
	MonitorIntervalSec *int `json:"monitor_interval_sec"`
	IDDigits           *int `json:"id_digits"`
	ClaimCodeLength    *int `json:"claim_code_length"`
}

func portFree(p int) bool {
	l, err := net.Listen("tcp", ":"+strconv.Itoa(p))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

func settingsHandler(store *config.Store, collector *metrics.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, viewOf(store.Get()))

		case http.MethodPatch:
			var p settingsPatch
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			old := store.Get()

			// занятость проверяем только у портов, которые реально меняются
			if p.HTTPPort != nil && *p.HTTPPort != old.HTTPPort && !portFree(*p.HTTPPort) {
				writeError(w, http.StatusBadRequest, "HTTP порт "+strconv.Itoa(*p.HTTPPort)+" уже занят")
				return
			}
			if p.P2PPort != nil && *p.P2PPort != old.P2PPort && !portFree(*p.P2PPort) {
				writeError(w, http.StatusBadRequest, "P2P порт "+strconv.Itoa(*p.P2PPort)+" уже занят")
				return
			}

			updated, err := store.Update(func(c *config.Config) {
				if p.HTTPPort != nil {
					c.HTTPPort = *p.HTTPPort
				}
				if p.P2PPort != nil {
					c.P2PPort = *p.P2PPort
				}
				if p.MonitorIntervalSec != nil {
					c.MonitorIntervalSec = *p.MonitorIntervalSec
				}
				if p.IDDigits != nil {
					c.IDDigits = *p.IDDigits
				}
				if p.ClaimCodeLength != nil {
					c.ClaimCodeLength = *p.ClaimCodeLength
				}
			})
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			collector.SetPeriod(time.Duration(updated.MonitorIntervalSec) * time.Second)

			writeJSON(w, http.StatusOK, map[string]any{
				"settings":         viewOf(updated),
				"restart_required": updated.HTTPPort != old.HTTPPort || updated.P2PPort != old.P2PPort,
			})

		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
