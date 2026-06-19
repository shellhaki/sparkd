package internals

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DBTypePostgres = "pg"

	CellStateCreating = "creating"
	CellStateRunning  = "running"
	CellStatePaused   = "paused"
	CellStateStopped  = "stopped"
	CellStateError    = "error"
	CellStateDeleted  = "deleted"
)

type SizeMB int

func (s *SizeMB) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*s = 0
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := ParseSizeMB(value)
		if err != nil {
			return err
		}
		*s = SizeMB(parsed)
		return nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("size must be a number of megabytes or a string like 100mb: %w", err)
	}
	*s = SizeMB(value)
	return nil
}

func ParseSizeMB(value string) (int, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, " ", "")
	if normalized == "" {
		return 0, nil
	}

	multiplier := 1
	switch {
	case strings.HasSuffix(normalized, "mb"):
		normalized = strings.TrimSuffix(normalized, "mb")
	case strings.HasSuffix(normalized, "m"):
		normalized = strings.TrimSuffix(normalized, "m")
	case strings.HasSuffix(normalized, "gb"):
		normalized = strings.TrimSuffix(normalized, "gb")
		multiplier = 1024
	case strings.HasSuffix(normalized, "g"):
		normalized = strings.TrimSuffix(normalized, "g")
		multiplier = 1024
	}

	number, err := strconv.Atoi(normalized)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", value)
	}
	return number * multiplier, nil
}

type CreateCellRequest struct {
	Name   string `json:"name"`
	DBType string `json:"dbtype"`
	Port   int    `json:"port"`
	RAM    SizeMB `json:"ram"`
	Disk   SizeMB `json:"disk"`
}

type CreateCellResponse struct {
	Cell             Cell            `json:"cell"`
	Credentials      Credentials     `json:"credentials"`
	ConnectionString string          `json:"connection_string"`
	Progress         []ProgressEvent `json:"progress"`
}

type Credentials struct {
	Host             string `json:"host"`
	Port             int    `json:"port"`
	Database         string `json:"database"`
	User             string `json:"user"`
	Password         string `json:"password"`
	ConnectionString string `json:"connection_string"`
}

type Cell struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DBType    string `json:"dbtype"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
	RootFS    string `json:"rootfs"`
	PID       int    `json:"pid,omitempty"`
	CellIP    string `json:"cell_ip"`
	HostIP    string `json:"host_ip"`
	HostPort  int    `json:"host_port"`
	InnerPort int    `json:"inner_port"`

	RAMMB    int `json:"ram_mb"`
	DiskMB   int `json:"disk_mb"`
	CPUMilli int `json:"cpu_milli"`

	Database         string          `json:"database"`
	User             string          `json:"user"`
	Password         string          `json:"password"`
	ConnectionString string          `json:"connection_string"`
	Events           []ProgressEvent `json:"events,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProgressEvent struct {
	Time    time.Time `json:"time"`
	Stage   string    `json:"stage"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

type ProgressLog struct {
	mu     sync.Mutex
	events []ProgressEvent
}

func NewProgressLog() *ProgressLog {
	return &ProgressLog{}
}

func (p *ProgressLog) Add(stage, status, message string) ProgressEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	event := ProgressEvent{
		Time:    time.Now().UTC(),
		Stage:   stage,
		Status:  status,
		Message: message,
	}
	p.events = append(p.events, event)
	fmt.Printf("[%s] %s: %s\n", stage, status, message)
	return event
}

func (p *ProgressLog) AddEvent(event ProgressEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	p.events = append(p.events, event)
	fmt.Printf("[%s] %s: %s\n", event.Stage, event.Status, event.Message)
}

func (p *ProgressLog) Events() []ProgressEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	events := make([]ProgressEvent, len(p.events))
	copy(events, p.events)
	return events
}

func normalizeDBType(dbType string) string {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "", "pg", "postgres", "postgresql":
		return DBTypePostgres
	default:
		return strings.ToLower(strings.TrimSpace(dbType))
	}
}

func inferCPUMilliFromRAM(ramMB int) int {
	if ramMB <= 0 {
		return 250
	}
	cpu := ramMB * 2
	if cpu < 100 {
		return 100
	}
	if cpu > 4000 {
		return 4000
	}
	return cpu
}

func sanitizeCellName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}

	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		return "pg-" + shortID()
	}
	return clean
}

func sqlSafeName(prefix, id string) string {
	clean := strings.ReplaceAll(sanitizeCellName(id), "-", "_")
	if clean == "" || (clean[0] >= '0' && clean[0] <= '9') {
		clean = prefix + "_" + clean
	}
	return clean
}

func shortID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func randomPassword() string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "sparkd-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func detectHostIP() string {
	if value := osHostnameOverride(); value != "" {
		return value
	}

	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok && addr.IP != nil {
			return addr.IP.String()
		}
	}

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() {
				continue
			}
			ip := ipNet.IP.To4()
			if ip != nil {
				return ip.String()
			}
		}
	}

	return "127.0.0.1"
}

func osHostnameOverride() string {
	return strings.TrimSpace(getenv("SPARKD_HOST", ""))
}
