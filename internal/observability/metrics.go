package observability

import (
	"fmt"
	"io"
	"sync"

	"github.com/example/game-server-control-plane/internal/model"
)

type ServerMetrics struct {
	CPUPercent  float64
	MemoryBytes int64
	Uptime      float64
	Healthy     bool
}

type Metrics struct {
	mu             sync.RWMutex
	servers        map[string]ServerMetrics
	restarts       uint64
	updates        uint64
	updateFailures uint64
	total          int
	running        int
}

func NewMetrics() *Metrics {
	return &Metrics{servers: map[string]ServerMetrics{}}
}

func (m *Metrics) SetFleet(total, running int) {
	m.mu.Lock()
	m.total = total
	m.running = running
	m.mu.Unlock()
}

func (m *Metrics) ObserveServer(id string, status model.RuntimeStatus) {
	m.mu.Lock()
	m.servers[id] = ServerMetrics{CPUPercent: status.CPUPercent, MemoryBytes: status.MemoryBytes, Uptime: status.Uptime.Seconds(), Healthy: status.Healthy}
	m.mu.Unlock()
}

func (m *Metrics) IncRestart() {
	m.mu.Lock()
	m.restarts++
	m.mu.Unlock()
}

func (m *Metrics) IncUpdate(success bool) {
	m.mu.Lock()
	m.updates++
	if !success {
		m.updateFailures++
	}
	m.mu.Unlock()
}

func (m *Metrics) WritePrometheus(w io.Writer) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fmt.Fprintf(w, "# TYPE managed_servers_total gauge\nmanaged_servers_total %d\n", m.total)
	fmt.Fprintf(w, "# TYPE managed_servers_running gauge\nmanaged_servers_running %d\n", m.running)
	fmt.Fprintf(w, "# TYPE server_restart_total counter\nserver_restart_total %d\n", m.restarts)
	fmt.Fprintf(w, "# TYPE server_update_total counter\nserver_update_total %d\n", m.updates)
	fmt.Fprintf(w, "# TYPE server_update_failures_total counter\nserver_update_failures_total %d\n", m.updateFailures)
	fmt.Fprintln(w, "# TYPE server_cpu_percent gauge")
	fmt.Fprintln(w, "# TYPE server_memory_bytes gauge")
	fmt.Fprintln(w, "# TYPE server_uptime_seconds gauge")
	fmt.Fprintln(w, "# TYPE server_responsive gauge")
	for id, value := range m.servers {
		fmt.Fprintf(w, "server_cpu_percent{server_id=%q} %f\n", id, value.CPUPercent)
		fmt.Fprintf(w, "server_memory_bytes{server_id=%q} %d\n", id, value.MemoryBytes)
		fmt.Fprintf(w, "server_uptime_seconds{server_id=%q} %f\n", id, value.Uptime)
		responsive := 0
		if value.Healthy {
			responsive = 1
		}
		fmt.Fprintf(w, "server_responsive{server_id=%q} %d\n", id, responsive)
	}
}
