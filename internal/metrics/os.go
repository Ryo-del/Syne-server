package metrics

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

const (
	maxPoints     = 60
	collectPeriod = time.Second
)

type Point struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type NetworkPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Download  float64   `json:"download"`
	Upload    float64   `json:"upload"`
}

type MetricResponse struct {
	Metric string  `json:"metric"`
	Unit   string  `json:"unit"`
	Total  float64 `json:"total,omitempty"`
	Points []Point `json:"points"`
}

type NetworkResponse struct {
	Metric string         `json:"metric"`
	Unit   string         `json:"unit"`
	Points []NetworkPoint `json:"points"`
}

type Collector struct {
	mu sync.RWMutex

	cpu     []Point
	ram     []Point
	disk    []Point
	network []NetworkPoint

	lastRx uint64
	lastTx uint64
}

func NewCollector() *Collector {
	c := &Collector{}

	go c.collectLoop()

	return c
}

func (c *Collector) collectLoop() {
	ticker := time.NewTicker(collectPeriod)
	defer ticker.Stop()

	for range ticker.C {
		c.collect()
	}
}

func (c *Collector) collect() {
	now := time.Now()

	// CPU
	cpuUsage, err := cpu.Percent(0, false)
	if err == nil && len(cpuUsage) > 0 {
		c.addCPU(Point{
			Timestamp: now,
			Value:     cpuUsage[0],
		})
	}

	// RAM
	if memory, err := mem.VirtualMemory(); err == nil {
		usedGB := bytesToGB(memory.Used)

		c.addRAM(Point{
			Timestamp: now,
			Value:     usedGB,
		})
	}

	// Disk
	if usage, err := disk.Usage("/"); err == nil {
		usedGB := bytesToGB(usage.Used)

		c.addDisk(Point{
			Timestamp: now,
			Value:     usedGB,
		})
	}

	// Network
	c.collectNetwork(now)
}

func (c *Collector) collectNetwork(now time.Time) {
	stats, err := net.IOCounters(false)
	if err != nil || len(stats) == 0 {
		return
	}

	rx := stats[0].BytesRecv
	tx := stats[0].BytesSent

	// Первое измерение нужно только для создания базы сравнения.
	if c.lastRx == 0 {
		c.lastRx = rx
		c.lastTx = tx
		return
	}

	download := float64(rx-c.lastRx) / collectPeriod.Seconds()
	upload := float64(tx-c.lastTx) / collectPeriod.Seconds()

	c.lastRx = rx
	c.lastTx = tx

	c.mu.Lock()

	c.network = append(c.network, NetworkPoint{
		Timestamp: now,
		Download:  bytesToMB(download),
		Upload:    bytesToMB(upload),
	})

	if len(c.network) > maxPoints {
		c.network = c.network[len(c.network)-maxPoints:]
	}

	c.mu.Unlock()
}

func (c *Collector) addCPU(point Point) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cpu = append(c.cpu, point)

	if len(c.cpu) > maxPoints {
		c.cpu = c.cpu[len(c.cpu)-maxPoints:]
	}
}

func (c *Collector) addRAM(point Point) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ram = append(c.ram, point)

	if len(c.ram) > maxPoints {
		c.ram = c.ram[len(c.ram)-maxPoints:]
	}
}

func (c *Collector) addDisk(point Point) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.disk = append(c.disk, point)

	if len(c.disk) > maxPoints {
		c.disk = c.disk[len(c.disk)-maxPoints:]
	}
}

func (c *Collector) CPUHandler(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	points := append([]Point(nil), c.cpu...)
	c.mu.RUnlock()

	writeJSON(w, MetricResponse{
		Metric: "cpu",
		Unit:   "%",
		Points: points,
	})
}

func (c *Collector) RAMHandler(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	points := append([]Point(nil), c.ram...)
	c.mu.RUnlock()

	total := 0.0

	if memory, err := mem.VirtualMemory(); err == nil {
		total = bytesToGB(memory.Total)
	}

	writeJSON(w, MetricResponse{
		Metric: "ram",
		Unit:   "GB",
		Total:  total,
		Points: points,
	})
}

func (c *Collector) DiskHandler(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	points := append([]Point(nil), c.disk...)
	c.mu.RUnlock()

	total := 0.0

	if usage, err := disk.Usage("/"); err == nil {
		total = bytesToGB(usage.Total)
	}

	writeJSON(w, MetricResponse{
		Metric: "disk",
		Unit:   "GB",
		Total:  total,
		Points: points,
	})
}

func (c *Collector) NetworkHandler(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	points := append([]NetworkPoint(nil), c.network...)
	c.mu.RUnlock()

	writeJSON(w, NetworkResponse{
		Metric: "network",
		Unit:   "MB/s",
		Points: points,
	})
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(data)
}

func bytesToGB(bytes uint64) float64 {
	return float64(bytes) / 1024 / 1024 / 1024
}

func bytesToMB(bytes float64) float64 {
	return bytes / 1024 / 1024
}

func RegisterRoutes(mux *http.ServeMux, collector *Collector) {
	mux.HandleFunc("/api/metrics/cpu", collector.CPUHandler)
	mux.HandleFunc("/api/metrics/ram", collector.RAMHandler)
	mux.HandleFunc("/api/metrics/disk", collector.DiskHandler)
	mux.HandleFunc("/api/metrics/network", collector.NetworkHandler)
}
