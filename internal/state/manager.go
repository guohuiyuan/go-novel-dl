package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/guohuiyuan/go-novel-dl/internal/config"
)

type FileState struct {
	Language string `json:"language,omitempty"`
}

type Manager struct {
	path string
}

func NewManager(appName string) *Manager {
	base := filepath.Join(config.DataDir, appName)
	return &Manager{path: filepath.Join(base, "state.json")}
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) Load() (FileState, error) {
	var state FileState

	data, err := os.ReadFile(m.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}

	if len(data) == 0 {
		return state, nil
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return FileState{}, err
	}

	return state, nil
}

func (m *Manager) Save(state FileState) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(m.path, data, 0o644)
}

func (m *Manager) SetLanguage(language string) error {
	state, err := m.Load()
	if err != nil {
		return err
	}

	state.Language = language
	return m.Save(state)
}
