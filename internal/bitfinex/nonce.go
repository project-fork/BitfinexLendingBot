package bitfinex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const nonceStateFileName = "nonce_state.json"

type nonceState struct {
	LastNonce uint64 `json:"last_nonce"`
}

type persistentNonceGenerator struct {
	path      string
	mu        sync.Mutex
	lastNonce uint64
}

func newPersistentNonceGenerator(statePath string) (*persistentNonceGenerator, error) {
	gen := &persistentNonceGenerator{
		path:      statePath,
		lastNonce: uint64(time.Now().UnixNano()),
	}

	if err := gen.load(); err != nil {
		return nil, err
	}

	return gen, nil
}

func defaultNonceStateFilePath() string {
	exePath, err := os.Executable()
	if err != nil {
		return nonceStateFileName
	}
	return filepath.Join(filepath.Dir(exePath), nonceStateFileName)
}

func (g *persistentNonceGenerator) GetNonce() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	current := uint64(time.Now().UnixNano())
	if current <= g.lastNonce {
		current = g.lastNonce + 1
	}
	g.lastNonce = current
	g.saveLocked()
	return strconv.FormatUint(g.lastNonce, 10)
}

func (g *persistentNonceGenerator) load() error {
	data, err := os.ReadFile(g.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var state nonceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}
	if state.LastNonce > g.lastNonce {
		g.lastNonce = state.LastNonce
	}
	return nil
}

func (g *persistentNonceGenerator) saveLocked() {
	if g.path == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(g.path), 0700); err != nil {
		return
	}

	payload, err := json.MarshalIndent(nonceState{LastNonce: g.lastNonce}, "", "  ")
	if err != nil {
		return
	}
	payload = append(payload, '\n')

	tmpPath := g.path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0600); err != nil {
		return
	}
	_ = os.Rename(tmpPath, g.path)
}
