package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"gopkg.in/yaml.v3"
)

const disabledHeader = `# Сервисы, которые sudoku пропускает при start / restart / rebuild / build.
# Ключ — путь к compose-файлу относительно ROOT_PROJECTS_FOLDER, значение — список сервисов.
# Файл можно править руками или из админки (sudoku serve).
`

type disabledFile struct {
	Disabled map[string][]string `yaml:"disabled"`
}

type DisabledStore struct {
	mu    sync.Mutex
	root  string
	path  string
	data  map[string][]string
	mtime int64
}

func DisabledPath(root string) string {
	return filepath.Join(root, "sudoku-config", "disabled.yaml")
}

func NewDisabledStore(root string) *DisabledStore {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return &DisabledStore{root: abs, path: DisabledPath(abs), data: map[string][]string{}}
}

func (d *DisabledStore) Path() string {
	return d.path
}

func (d *DisabledStore) Rel(composeFile string) string {
	rel, err := filepath.Rel(d.root, composeFile)
	if err != nil {
		return composeFile
	}
	return filepath.ToSlash(rel)
}

func (d *DisabledStore) reloadLocked() error {
	st, err := os.Stat(d.path)
	if errors.Is(err, os.ErrNotExist) {
		d.data, d.mtime = map[string][]string{}, 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", d.path, err)
	}
	if st.ModTime().UnixNano() == d.mtime {
		return nil
	}
	raw, err := os.ReadFile(d.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", d.path, err)
	}
	var f disabledFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse %s: %w", d.path, err)
	}
	if f.Disabled == nil {
		f.Disabled = map[string][]string{}
	}
	d.data, d.mtime = f.Disabled, st.ModTime().UnixNano()
	return nil
}

func (d *DisabledStore) For(composeFile string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.reloadLocked(); err != nil {
		return nil, err
	}
	return append([]string(nil), d.data[d.Rel(composeFile)]...), nil
}

func (d *DisabledStore) Set(composeFile, service string, disabled bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.reloadLocked(); err != nil {
		return err
	}
	rel := d.Rel(composeFile)
	set := map[string]bool{}
	for _, s := range d.data[rel] {
		set[s] = true
	}
	if disabled {
		set[service] = true
	} else {
		delete(set, service)
	}
	list := make([]string, 0, len(set))
	for s := range set {
		list = append(list, s)
	}
	sort.Strings(list)
	if len(list) == 0 {
		delete(d.data, rel)
	} else {
		d.data[rel] = list
	}
	return d.writeLocked()
}

func (d *DisabledStore) writeLocked() error {
	var buf bytes.Buffer
	buf.WriteString(disabledHeader)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(disabledFile{Disabled: d.data}); err != nil {
		return fmt.Errorf("encode disabled: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode disabled: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := d.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil { //nolint:gosec // plain config file next to the user's compose files
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, d.path); err != nil {
		return fmt.Errorf("replace %s: %w", d.path, err)
	}
	if st, err := os.Stat(d.path); err == nil {
		d.mtime = st.ModTime().UnixNano()
	}
	return nil
}

func EnabledServices(all, disabled []string) []string {
	off := map[string]bool{}
	for _, s := range disabled {
		off[s] = true
	}
	out := make([]string, 0, len(all))
	for _, s := range all {
		if !off[s] {
			out = append(out, s)
		}
	}
	return out
}
