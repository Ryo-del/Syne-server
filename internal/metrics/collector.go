package metrics

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

const maxPoints = 60

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
	lastAt time.Time

	period atomic.Int64 // ns
	wake   chan struct{}
}

func systemDiskPath() string {
	if runtime.GOOS == "windows" {
		return "C:\\"
	}

	return "/"
}

func NewCollector(period time.Duration) *Collector {
	c := &Collector{wake: make(chan struct{}, 1)}
	c.period.Store(int64(period))

	go c.collectLoop()

	return c
}

func (c *Collector) Period() time.Duration { return time.Duration(c.period.Load()) }

// SetPeriod меняет интервал без перезапуска.
func (c *Collector) SetPeriod(d time.Duration) {
	c.period.Store(int64(d))
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *Collector) collectLoop() {
	timer := time.NewTimer(c.Period())
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			c.collect()
		case <-c.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		timer.Reset(c.Period())
	}
}

func (c *Collector) collect() {
	now := time.Now()

	// CPU
	cpuUsage, err := cpu.Percent(0, false)
	if err == nil && len(cpuUsage) > 0 {
		c.addCPU(Point{Timestamp: now, Value: cpuUsage[0]})
	}

	// RAM
	if memory, err := mem.VirtualMemory(); err == nil {
		c.addRAM(Point{Timestamp: now, Value: bytesToGB(memory.Used)})
	}

	// Disk
	if usage, err := disk.Usage(systemDiskPath()); err == nil {
		c.addDisk(Point{Timestamp: now, Value: bytesToGB(usage.Used)})
	}

	// Network
	c.collectNetwork(now)
}

func (c *Collector) collectNetwork(now time.Time) {
	stats, err := net.IOCounters(false)
	if err != nil || len(stats) == 0 {
		return
	}
	rx, tx := stats[0].BytesRecv, stats[0].BytesSent

	// первое измерение — только база для сравнения
	if c.lastAt.IsZero() || rx < c.lastRx || tx < c.lastTx {
		c.lastRx, c.lastTx, c.lastAt = rx, tx, now
		return
	}

	// считаем по реальному времени между замерами, а не по константе
	elapsed := now.Sub(c.lastAt).Seconds()
	download := float64(rx-c.lastRx) / elapsed
	upload := float64(tx-c.lastTx) / elapsed
	c.lastRx, c.lastTx, c.lastAt = rx, tx, now

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
	if usage, err := disk.Usage(systemDiskPath()); err == nil {
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
