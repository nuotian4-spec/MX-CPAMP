package management

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginstore"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/updatestate"
)

func (h *Handler) preparePluginBackup(m *updateMutation, id string) error {
	store, err := h.updateStore()
	if err != nil {
		return err
	}
	m.backupRoot = filepath.Join(store.Directory, "backups", m.lease.Operation().ID)
	m.replacedFiles = make(map[string]string)
	if err = os.MkdirAll(m.backupRoot, 0o700); err != nil {
		return err
	}
	if err = copyPluginBackup(h.configFilePath, filepath.Join(m.backupRoot, "config.yaml")); err != nil {
		return err
	}
	h.mu.Lock()
	if h.cfg != nil {
		m.previous = h.cfg.CloneForRuntime()
	}
	if h.pluginHost != nil {
		m.previousVersion, m.previousPath, m.previousActive = h.pluginHost.ActivePluginIdentity(id)
	}
	h.mu.Unlock()
	if m.previousActive {
		if err = copyPluginBackup(m.previousPath, filepath.Join(m.backupRoot, filepath.Base(m.previousPath))); err != nil {
			return err
		}
	}
	return updatestate.WriteJSON(filepath.Join(m.backupRoot, "original.json"), map[string]any{
		"plugin_id": id, "active_path": m.previousPath, "active_version": m.previousVersion, "registered": m.previousActive,
		"configuration": h.configFilePath, "database_restore": false,
	})
}

func (m *updateMutation) backupReplacedPlugin(path string) error {
	if m.previousActive && filepath.Clean(path) == filepath.Clean(m.previousPath) {
		return pluginstore.ErrLoadedPluginLocked
	}
	backup := filepath.Join(m.backupRoot, filepath.Base(path))
	if err := copyPluginBackup(path, backup); err != nil {
		return err
	}
	m.replacedFiles[path] = backup
	return updatestate.WriteJSON(filepath.Join(m.backupRoot, "replaced-files.json"), m.replacedFiles)
}

func copyPluginBackup(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	if err == nil {
		err = output.Sync()
	}
	return errors.Join(err, output.Close())
}

func (m *updateMutation) restoreReplacedPlugins() error {
	for target, backup := range m.replacedFiles {
		input, err := os.ReadFile(backup)
		if err != nil {
			return err
		}
		temporary, err := os.CreateTemp(filepath.Dir(target), ".restore-plugin-*.tmp")
		if err != nil {
			return err
		}
		name := temporary.Name()
		if err = temporary.Chmod(0o755); err == nil {
			_, err = temporary.Write(input)
		}
		if err == nil {
			err = temporary.Sync()
		}
		err = errors.Join(err, temporary.Close())
		if err == nil {
			err = os.Rename(name, target)
		}
		_ = os.Remove(name)
		if err != nil {
			return err
		}
	}
	return nil
}
