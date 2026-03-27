package node

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	panel "github.com/keli-123456/kelinode/api/v2board"
	"github.com/keli-123456/kelinode/conf"
)

type userSyncStateFile struct {
	Revision  int64            `json:"revision"`
	Users     []panel.UserInfo `json:"users"`
	UpdatedAt time.Time        `json:"updated_at"`
}

func userSyncStateDir() string {
	if v := os.Getenv("V2NODE_STATE_DIR"); v != "" {
		return v
	}
	return "/etc/v2node"
}

func userSyncStatePath(configDir string, apiHost string, nodeID int) string {
	sum := sha1.Sum([]byte(apiHost))
	baseDir := conf.NormalizeConfigDir(configDir)
	if baseDir == conf.DefaultNodeConfigDir {
		baseDir = userSyncStateDir()
	}
	return filepath.Join(baseDir, fmt.Sprintf("user_sync_%s_%d.json", hex.EncodeToString(sum[:]), nodeID))
}

func loadUserSyncState(path string) (*userSyncStateFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	state := &userSyncStateFile{}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, err
	}
	return state, nil
}

func saveUserSyncState(path string, state *userSyncStateFile) error {
	if state == nil {
		return nil
	}
	state.UpdatedAt = time.Now()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
