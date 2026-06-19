package internals

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Daemon struct {
	cfg      Config
	store    *Store
	hostIP   string
	children map[string]*exec.Cmd
	mu       sync.Mutex
}

func NewDaemon(cfg Config) (*Daemon, error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}
	store, err := NewStore(cfg.StorePath)
	if err != nil {
		return nil, err
	}
	return &Daemon{
		cfg:      cfg,
		store:    store,
		hostIP:   detectHostIP(),
		children: make(map[string]*exec.Cmd),
	}, nil
}

func (d *Daemon) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/create", d.handleCreate)
	mux.HandleFunc("/list", d.handleList)
	mux.HandleFunc("/pause", d.handlePause)
	mux.HandleFunc("/resume", d.handleResume)
	mux.HandleFunc("/monitor", d.handleMonitor)
	mux.HandleFunc("/delete", d.handleDelete)
	mux.HandleFunc("/health", d.handleHealth)

	fmt.Println("sparkd listening on tcp:", d.cfg.ListenAddr)
	return http.ListenAndServe(d.cfg.ListenAddr, jsonOnlyMux(mux))
}

func (d *Daemon) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"addr":   d.cfg.ListenAddr,
		"port":   listenPort(d.cfg.ListenAddr),
	})
}

func (d *Daemon) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "use POST /create")
		return
	}

	var req CreateCellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cell, credentials, progress, err := d.CreateCell(req)
	if err != nil {
		writeErrorWithProgress(w, http.StatusInternalServerError, err.Error(), progress)
		return
	}

	writeJSON(w, http.StatusCreated, CreateCellResponse{
		Cell:             cell,
		Credentials:      credentials,
		ConnectionString: credentials.ConnectionString,
		Progress:         progress,
	})
}

func (d *Daemon) handleList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"cells": d.store.List(),
	})
}

func (d *Daemon) handleMonitor(w http.ResponseWriter, r *http.Request) {
	name := sanitizeCellName(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"cells": d.store.List(),
		})
		return
	}

	cell, ok := d.store.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "cell not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cell": cell,
	})
}

func (d *Daemon) handlePause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "use POST /pause")
		return
	}

	name, err := requestCellName(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cell, err := d.pauseCell(name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cell": cell})
}

func (d *Daemon) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "use POST /resume")
		return
	}

	name, err := requestCellName(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cell, progress, err := d.resumeCell(name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeErrorWithProgress(w, status, err.Error(), progress)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cell":     cell,
		"progress": progress,
	})
}

func (d *Daemon) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "use POST or DELETE /delete")
		return
	}

	name, err := requestCellName(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := d.deleteCell(name); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": name})
}

func (d *Daemon) CreateCell(req CreateCellRequest) (Cell, Credentials, []ProgressEvent, error) {
	progress := NewProgressLog()

	req.DBType = normalizeDBType(req.DBType)
	if req.DBType != DBTypePostgres {
		return Cell{}, Credentials{}, nil, fmt.Errorf("only postgres cells are supported right now")
	}

	name := sanitizeCellName(req.Name)
	if _, ok := d.store.Get(name); ok {
		return Cell{}, Credentials{}, nil, fmt.Errorf("cell %q already exists", name)
	}

	ramMB := int(req.RAM)
	if ramMB == 0 {
		ramMB = 128
	}
	diskMB := int(req.Disk)
	if diskMB == 0 {
		diskMB = 256
	}

	if err := EnsureBaseRootFS(d.cfg, progress); err != nil {
		return Cell{}, Credentials{}, progress.Events(), err
	}
	if err := ImportDatabase(d.cfg, req.DBType, progress); err != nil {
		return Cell{}, Credentials{}, progress.Events(), err
	}

	plan, err := PlanCellNetwork(d.store.List(), req.Port)
	if err != nil {
		return Cell{}, Credentials{}, progress.Events(), err
	}

	rootfs, err := CreateCellRootFS(d.cfg, name, diskMB, progress)
	if err != nil {
		return Cell{}, Credentials{}, progress.Events(), err
	}

	id := shortID()
	dbName := sqlSafeName("db", name)
	dbUser := "postgres"
	password := randomPassword()
	connectionString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", dbUser, password, d.hostIP, plan.HostPort, dbName)

	now := time.Now().UTC()
	cell := Cell{
		ID:               id,
		Name:             name,
		DBType:           req.DBType,
		State:            CellStateCreating,
		RootFS:           rootfs,
		CellIP:           plan.CellIP,
		HostIP:           d.hostIP,
		HostPort:         plan.HostPort,
		InnerPort:        plan.InnerPort,
		RAMMB:            ramMB,
		DiskMB:           diskMB,
		CPUMilli:         inferCPUMilliFromRAM(ramMB),
		Database:         dbName,
		User:             dbUser,
		Password:         password,
		ConnectionString: connectionString,
		Events:           progress.Events(),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := d.store.Save(cell); err != nil {
		return Cell{}, Credentials{}, progress.Events(), err
	}

	cmd, startupEvents, err := d.startCellProcess(cell)
	for _, event := range startupEvents {
		progress.AddEvent(event)
	}
	cell.Events = progress.Events()

	if err != nil {
		cell.State = CellStateError
		cell.Error = err.Error()
		_ = d.store.Save(cell)
		return cell, Credentials{}, progress.Events(), err
	}

	cell.PID = cmd.Process.Pid
	cell.State = CellStateRunning
	cell.UpdatedAt = time.Now().UTC()
	cell.Events = progress.Events()
	if err := d.store.Save(cell); err != nil {
		return Cell{}, Credentials{}, progress.Events(), err
	}

	d.mu.Lock()
	d.children[cell.Name] = cmd
	d.mu.Unlock()

	credentials := Credentials{
		Host:             d.hostIP,
		Port:             plan.HostPort,
		Database:         dbName,
		User:             dbUser,
		Password:         password,
		ConnectionString: connectionString,
	}

	return cell, credentials, progress.Events(), nil
}

func (d *Daemon) startCellProcess(cell Cell) (*exec.Cmd, []ProgressEvent, error) {
	progress := NewProgressLog()
	progress.Add("cell-process", "running", "starting isolated cell process")

	rPipe, wPipe, err := os.Pipe()
	if err != nil {
		return nil, progress.Events(), err
	}
	defer rPipe.Close()

	cmd := exec.Command("/proc/self/exe", "cell-child", cell.RootFS)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWPID |
			syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWNET,
	}
	cmd.Env = append(os.Environ(),
		"SPARKD_DB_TYPE="+cell.DBType,
		"SPARKD_DB_NAME="+cell.Database,
		"SPARKD_DB_USER="+cell.User,
		"SPARKD_DB_PASSWORD="+cell.Password,
	)
	cmd.ExtraFiles = []*os.File{rPipe}
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = wPipe.Close()
		return nil, progress.Events(), err
	}

	if err := cmd.Start(); err != nil {
		_ = wPipe.Close()
		return nil, progress.Events(), err
	}
	rPipe.Close()

	cell.PID = cmd.Process.Pid
	progress.Add("resources", "running", "attaching cell to cgroup resource limits")
	if err := ApplyCellResources(cell); err != nil {
		_ = wPipe.Close()
		_ = cmd.Process.Kill()
		return nil, progress.Events(), err
	}

	progress.Add("network", "running", "configuring bridge, veth, and port forwarding")
	if err := SetupCellNetwork(d.cfg, cmd.Process.Pid, cell); err != nil {
		_ = wPipe.Close()
		_ = cmd.Process.Kill()
		return nil, progress.Events(), err
	}

	if _, err := wPipe.Write([]byte{1}); err != nil {
		_ = wPipe.Close()
		_ = cmd.Process.Kill()
		return nil, progress.Events(), err
	}
	_ = wPipe.Close()

	ready := make(chan error, 1)
	go readCellStartup(stdout, progress, ready)

	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, progress.Events(), err
		}
	case <-time.After(45 * time.Second):
		_ = cmd.Process.Kill()
		return nil, progress.Events(), fmt.Errorf("timed out waiting for cell readiness")
	}

	go d.reapCell(cell.Name, cmd)
	return cmd, progress.Events(), nil
}

func readCellStartup(stdout io.ReadCloser, progress *ProgressLog, ready chan<- error) {
	reportedReady := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "SPARKD_READY" {
			progress.Add("cell-process", "done", "cell reported ready")
			ready <- nil
			reportedReady = true
			continue
		}
		progress.Add("cell-log", "running", line)
	}
	if reportedReady {
		return
	}
	if err := scanner.Err(); err != nil {
		ready <- err
		return
	}
	ready <- fmt.Errorf("cell exited before reporting ready")
}

func (d *Daemon) reapCell(name string, cmd *exec.Cmd) {
	err := cmd.Wait()

	d.mu.Lock()
	delete(d.children, name)
	d.mu.Unlock()

	cell, ok := d.store.Get(name)
	if !ok || cell.State == CellStateDeleted || cell.State == CellStatePaused {
		return
	}
	if err != nil {
		cell.State = CellStateError
		cell.Error = err.Error()
	} else {
		cell.State = CellStateStopped
	}
	cell.PID = 0
	_ = d.store.Save(cell)
}

func (d *Daemon) pauseCell(name string) (Cell, error) {
	cell, ok := d.store.Get(name)
	if !ok {
		return Cell{}, os.ErrNotExist
	}

	d.mu.Lock()
	cmd := d.children[name]
	delete(d.children, name)
	d.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	DeleteCellNetwork(cell)
	RemoveCellResources(cell)

	cell.State = CellStatePaused
	cell.PID = 0
	cell.UpdatedAt = time.Now().UTC()
	return cell, d.store.Save(cell)
}

func (d *Daemon) resumeCell(name string) (Cell, []ProgressEvent, error) {
	cell, ok := d.store.Get(name)
	if !ok {
		return Cell{}, nil, os.ErrNotExist
	}
	if cell.State == CellStateRunning {
		return cell, cell.Events, nil
	}
	if cell.State == CellStateDeleted {
		return Cell{}, nil, os.ErrNotExist
	}

	progress := NewProgressLog()
	if err := EnsureCellRootFSMounted(cell); err != nil {
		cell.State = CellStateError
		cell.Error = err.Error()
		_ = d.store.Save(cell)
		return cell, progress.Events(), err
	}

	cell.State = CellStateCreating
	cell.Error = ""
	cell.Events = progress.Events()
	if err := d.store.Save(cell); err != nil {
		return cell, progress.Events(), err
	}

	cmd, startupEvents, err := d.startCellProcess(cell)
	for _, event := range startupEvents {
		progress.AddEvent(event)
	}
	cell.Events = progress.Events()
	if err != nil {
		cell.State = CellStateError
		cell.Error = err.Error()
		_ = d.store.Save(cell)
		return cell, progress.Events(), err
	}

	cell.PID = cmd.Process.Pid
	cell.State = CellStateRunning
	cell.UpdatedAt = time.Now().UTC()
	cell.Events = progress.Events()
	if err := d.store.Save(cell); err != nil {
		return cell, progress.Events(), err
	}

	d.mu.Lock()
	d.children[cell.Name] = cmd
	d.mu.Unlock()

	return cell, progress.Events(), nil
}

func (d *Daemon) deleteCell(name string) error {
	cell, ok := d.store.Get(name)
	if !ok {
		return os.ErrNotExist
	}

	_, _ = d.pauseCell(name)
	DeleteCellNetwork(cell)
	RemoveCellResources(cell)
	if cell.RootFS != "" {
		UnmountCellRootFS(cell.RootFS)
		if err := os.RemoveAll(filepath.Dir(cell.RootFS)); err != nil {
			return err
		}
	}
	return d.store.MarkDeleted(name)
}

func requestCellName(r *http.Request) (string, error) {
	name := sanitizeCellName(r.URL.Query().Get("name"))
	if name != "" {
		return name, nil
	}

	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return "", err
	}
	name = sanitizeCellName(payload.Name)
	if name == "" {
		return "", fmt.Errorf("missing cell name")
	}
	return name, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func writeErrorWithProgress(w http.ResponseWriter, status int, message string, progress []ProgressEvent) {
	writeJSON(w, status, map[string]any{
		"error":    message,
		"progress": progress,
	})
}

func jsonOnlyMux(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/create", "/list", "/pause", "/resume", "/monitor", "/delete", "/health":
			mux.ServeHTTP(w, r)
		default:
			writeError(w, http.StatusNotFound, "endpoint not found")
		}
	})
}

func listenPort(addr string) string {
	parts := strings.Split(addr, ":")
	if len(parts) == 0 {
		return addr
	}
	return parts[len(parts)-1]
}
