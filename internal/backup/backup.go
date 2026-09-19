package backup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wakeoneself/Baselock/internal/sys"
)

const RootDir = "/var/lib/sec/backups"

type Snapshot struct {
	ID        string            `json:"id"`
	Dir       string            `json:"dir"`
	Created   time.Time         `json:"created"`
	Modules   []string          `json:"modules"`
	Files     map[string]string `json:"files"` // dest path -> relative backup path or "" if missing
	Notes     map[string][]byte `json:"-"`
	NoteFiles map[string]string `json:"notes,omitempty"`
}

func New(modules []string) (*Snapshot, error) {
	id := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(RootDir, id)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		return nil, err
	}
	s := &Snapshot{
		ID:        id,
		Dir:       dir,
		Created:   time.Now().UTC(),
		Modules:   modules,
		Files:     map[string]string{},
		Notes:     map[string][]byte{},
		NoteFiles: map[string]string{},
	}
	return s, s.flushMeta()
}

func (s *Snapshot) SaveFile(path string) error {
	rel := strings.TrimPrefix(path, "/")
	dest := filepath.Join(s.Dir, "files", rel)
	if !sys.FileExists(path) {
		s.Files[path] = ""
		return s.flushMeta()
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := copyFile(path, dest); err != nil {
		return err
	}
	s.Files[path] = filepath.Join("files", rel)
	return s.flushMeta()
}

func (s *Snapshot) SaveNote(name string, data []byte) error {
	rel := filepath.Join("notes", name)
	if err := os.WriteFile(filepath.Join(s.Dir, rel), data, 0o600); err != nil {
		return err
	}
	s.Notes[name] = data
	s.NoteFiles[name] = rel
	return s.flushMeta()
}

func (s *Snapshot) RestoreFile(path string) error {
	rel, ok := s.Files[path]
	if !ok {
		return nil
	}
	if rel == "" {
		if sys.FileExists(path) {
			return os.Remove(path)
		}
		return nil
	}
	src := filepath.Join(s.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return copyFile(src, path)
}

func (s *Snapshot) Note(name string) ([]byte, error) {
	if data, ok := s.Notes[name]; ok {
		return data, nil
	}
	rel, ok := s.NoteFiles[name]
	if !ok {
		return nil, fmt.Errorf("note %s not in snapshot", name)
	}
	return os.ReadFile(filepath.Join(s.Dir, rel))
}

func (s *Snapshot) flushMeta() error {
	meta, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, "meta.json"), meta, 0o644)
}

func Latest() (*Snapshot, error) {
	list, err := List()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no backups in %s", RootDir)
	}
	return Load(list[0])
}

func List() ([]string, error) {
	entries, err := os.ReadDir(RootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}

func Load(id string) (*Snapshot, error) {
	dir := filepath.Join(RootDir, id)
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.Dir = dir
	s.Notes = map[string][]byte{}
	return &s, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
