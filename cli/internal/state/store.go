package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// BoardState stores credentials and metadata for a board.
type BoardState struct {
	AdminPassword   string   `json:"admin_password"`
	GeneralPassword string   `json:"general_password"`
	Teams           []string `json:"teams"`
	Size            [2]int   `json:"size"` // [cols, rows]
}

// Store manages board state on disk at ~/.bingo/boards.json.
type Store struct {
	path string
}

// NewStore creates a store using the default path.
func NewStore() *Store {
	home, _ := os.UserHomeDir()
	return &Store{path: filepath.Join(home, ".bingo", "boards.json")}
}

// NewStoreAt creates a store at a custom path (for testing).
func NewStoreAt(path string) *Store {
	return &Store{path: path}
}

// Load retrieves board state by name.
func (s *Store) Load(boardName string) (*BoardState, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var boards map[string]BoardState
	if err := json.Unmarshal(data, &boards); err != nil {
		return nil, err
	}
	board, ok := boards[boardName]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &board, nil
}

// Save stores board state by name.
func (s *Store) Save(boardName string, board BoardState) error {
	os.MkdirAll(filepath.Dir(s.path), 0755)

	var boards map[string]BoardState
	data, err := os.ReadFile(s.path)
	if err == nil {
		json.Unmarshal(data, &boards)
	}
	if boards == nil {
		boards = make(map[string]BoardState)
	}
	boards[boardName] = board

	out, _ := json.MarshalIndent(boards, "", "  ")
	return os.WriteFile(s.path, out, 0644)
}
