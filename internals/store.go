package internals

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	path  string
	mu    sync.Mutex
	cells map[string]Cell
}

type storedCells struct {
	Cells []Cell `json:"cells"`
}

func NewStore(path string) (*Store, error) {
	store := &Store{
		path:  path,
		cells: make(map[string]Cell),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}

	var state storedCells
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	for _, cell := range state.Cells {
		store.cells[cell.Name] = cell
	}
	return store, nil
}

func (s *Store) List() []Cell {
	s.mu.Lock()
	defer s.mu.Unlock()

	cells := make([]Cell, 0, len(s.cells))
	for _, cell := range s.cells {
		if cell.State != CellStateDeleted {
			cells = append(cells, cell)
		}
	}
	return cells
}

func (s *Store) Get(name string) (Cell, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cell, ok := s.cells[name]
	return cell, ok
}

func (s *Store) Save(cell Cell) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cell.UpdatedAt = time.Now().UTC()
	s.cells[cell.Name] = cell
	return s.flushLocked()
}

func (s *Store) MarkDeleted(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cell, ok := s.cells[name]
	if !ok {
		return os.ErrNotExist
	}
	cell.State = CellStateDeleted
	cell.UpdatedAt = time.Now().UTC()
	s.cells[name] = cell
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	state := storedCells{Cells: make([]Cell, 0, len(s.cells))}
	for _, cell := range s.cells {
		state.Cells = append(state.Cells, cell)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
