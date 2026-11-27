package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sudoku-golang/internal/fsutil"
	"sync"

	"gopkg.in/yaml.v3"
)

const setsHeader = `# Наборы проектов sudoku: sudoku start --set <имя> или кнопка в админке.
# targets — те же цели, что в CLI: проект (папка), infra, сервис, проект/сервис, шаблоны api-*.
`

const maxSetTargets = 200

var (
	ErrBadSetName  = errors.New("имя набора: латиница, цифры, - _ . (до 64 символов)")
	ErrSetNotFound = errors.New("набор не найден")
	ErrEmptySet    = errors.New("в наборе нет ни одной цели")
	setNameRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

type Set struct {
	Name        string   `yaml:"-" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description"`
	Targets     []string `yaml:"targets" json:"targets"`
}

type setsFile struct {
	Sets map[string]Set `yaml:"sets"`
}

// SetsStore is <data dir>/sudoku-config/sets.yaml, reloaded when its mtime changes (it may be edited by
// hand or by another sudoku process).
type SetsStore struct {
	mu    sync.Mutex
	path  string
	data  map[string]Set
	mtime int64
}

func SetsPath(l Layout) string {
	return filepath.Join(l.ConfigDir(), "sets.yaml")
}

func NewSetsStore(l Layout) *SetsStore {
	return &SetsStore{path: SetsPath(l), data: map[string]Set{}}
}

func (s *SetsStore) Path() string {
	return s.path
}

func (s *SetsStore) reloadLocked() error {
	st, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.data, s.mtime = map[string]Set{}, 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", s.path, err)
	}
	if st.ModTime().UnixNano() == s.mtime {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	var f setsFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	s.data = map[string]Set{}
	for name, set := range f.Sets {
		set.Name = name
		s.data[name] = set
	}
	s.mtime = st.ModTime().UnixNano()
	return nil
}

func (s *SetsStore) List() ([]Set, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return nil, err
	}
	out := make([]Set, 0, len(s.data))
	for _, set := range s.data {
		out = append(out, set)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *SetsStore) Get(name string) (Set, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return Set{}, err
	}
	set, ok := s.data[name]
	if !ok {
		return Set{}, fmt.Errorf("%w: %s", ErrSetNotFound, name)
	}
	return set, nil
}

func cleanTargets(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] && !strings.ContainsAny(t, "\r\n") {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Upsert creates or replaces a set. Targets are validated by the caller against the current projects.
func (s *SetsStore) Upsert(set Set) (Set, error) {
	if !setNameRe.MatchString(set.Name) {
		return Set{}, ErrBadSetName
	}
	set.Targets = cleanTargets(set.Targets)
	if len(set.Targets) == 0 {
		return Set{}, ErrEmptySet
	}
	if len(set.Targets) > maxSetTargets {
		set.Targets = set.Targets[:maxSetTargets]
	}
	set.Description = strings.TrimSpace(strings.ReplaceAll(set.Description, "\n", " "))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return Set{}, err
	}
	s.data[set.Name] = set
	return set, s.writeLocked()
}

func (s *SetsStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reloadLocked(); err != nil {
		return err
	}
	if _, ok := s.data[name]; !ok {
		return fmt.Errorf("%w: %s", ErrSetNotFound, name)
	}
	delete(s.data, name)
	return s.writeLocked()
}

func (s *SetsStore) writeLocked() error {
	var buf bytes.Buffer
	buf.WriteString(setsHeader)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	f := setsFile{Sets: s.data}
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encode sets: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("encode sets: %w", err)
	}
	if err := fsutil.WriteFileAtomic(s.path, buf.Bytes()); err != nil {
		return err //nolint:wrapcheck // already descriptive
	}
	if st, err := os.Stat(s.path); err == nil {
		s.mtime = st.ModTime().UnixNano()
	}
	return nil
}

// Expand replaces "@name" / set names given via --set with their targets.
func (s *SetsStore) Expand(names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		set, err := s.Get(strings.TrimPrefix(n, "@"))
		if err != nil {
			return nil, err
		}
		out = append(out, set.Targets...)
	}
	return out, nil
}
