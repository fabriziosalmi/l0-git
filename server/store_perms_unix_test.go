//go:build unix

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func modeOf(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// A permissive umask, on purpose: these tests assert that lgit makes its own
// files private rather than inheriting whatever the environment allows. Under a
// umask of 077 a world-readable database could not even be produced, and the
// assertions would pass with the feature removed.
func withPermissiveUmask(t *testing.T) {
	t.Helper()
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
}

func TestStore_NewFilesAreOwnerOnly(t *testing.T) {
	withPermissiveUmask(t)
	base := t.TempDir()
	path := filepath.Join(base, "created", "by", "lgit", "findings.db")
	t.Setenv("LGIT_DB", path)
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	plantLegacyRow(t, s, "g", "f:1:r", "m", 1) // forces the WAL and shm files to exist
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(path + suffix); err != nil {
			if suffix == "" {
				t.Fatalf("the database file must exist: %v", err)
			}
			continue
		}
		if m := modeOf(t, path+suffix); m&0o077 != 0 {
			t.Errorf("%s has mode %v, want no group/other access", filepath.Base(path+suffix), m)
		}
	}
	if m := modeOf(t, filepath.Dir(path)); m&0o077 != 0 {
		t.Errorf("a directory lgit created has mode %v, want 0700", m)
	}
}

// At the DEFAULT location an existing world-readable store is brought in line.
func TestStore_TightensAnExistingDefaultStore(t *testing.T) {
	withPermissiveUmask(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LGIT_DB", "")
	dir := filepath.Join(home, ".l0-git")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "findings.db")
	first, err := openStoreAt(db)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	if err := os.Chmod(db, 0o644); err != nil { // what earlier versions left behind
		t.Fatal(err)
	}
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m := modeOf(t, db); m != 0o600 {
		t.Errorf("an existing 0644 default store = %v, want 0600", m)
	}
	if m := modeOf(t, dir); m != 0o700 {
		t.Errorf("the default directory = %v, want 0700", m)
	}
}

// A database the user chose with LGIT_DB is theirs: it may be shared on purpose
// (a group-readable cache on a CI runner), so an EXISTING one is left exactly as
// it was, and so is the directory around it.
func TestStore_ExistingFileChosenWithLGITDBIsNotChmodded(t *testing.T) {
	withPermissiveUmask(t)
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "findings.db")
	first, err := openStoreAt(db)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Close()
	if err := os.Chmod(db, 0o664); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LGIT_DB", db)
	s, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m := modeOf(t, db); m != 0o664 {
		t.Errorf("a shared database chosen via LGIT_DB was chmod'ed to %v; it must be left alone", m)
	}
	if m := modeOf(t, dir); m != 0o775 {
		t.Errorf("the directory around it was chmod'ed to %v; it must be left alone", m)
	}
}

// tightenMode only ever REMOVES bits. A read-only file stays read-only: turning
// 0444 into 0600 would hand the owner write access nobody asked for.
func TestTightenMode_OnlyRemovesBits(t *testing.T) {
	for _, tc := range []struct {
		dir        bool
		from, want os.FileMode
	}{
		{false, 0o644, 0o600}, {false, 0o664, 0o600}, {false, 0o666, 0o600},
		{false, 0o444, 0o400}, {false, 0o600, 0o600}, {false, 0o400, 0o400},
		{true, 0o755, 0o700}, {true, 0o775, 0o700}, {true, 0o555, 0o500},
		{true, 0o700, 0o700}, {true, 0o500, 0o500},
	} {
		p := filepath.Join(t.TempDir(), "x")
		if tc.dir {
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, tc.from); err != nil {
			t.Fatal(err)
		}
		tightenMode(p)
		if got := modeOf(t, p); got != tc.want {
			t.Errorf("tightenMode(%v) -> %v, want %v", tc.from, got, tc.want)
		}
		_ = os.Chmod(p, 0o700) // let TempDir clean up
	}
}

// `~/.l0-git` may be a symlink to a shared directory. Chmod follows links, so
// without a Lstat the SHARED directory's mode would change.
func TestStoreDir_SymlinkedDefaultDirectoryIsNotFollowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LGIT_DB", "")
	shared := filepath.Join(t.TempDir(), "shared-store")
	if err := os.Mkdir(shared, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(home, ".l0-git")); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultDBPath(); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, shared); m != 0o775 {
		t.Errorf("the target of a symlinked ~/.l0-git was chmod'ed to %v", m)
	}
}

// A directory the user pointed LGIT_DB into is never chmod'ed.
func TestStoreDir_ExistingDirectoryChosenWithLGITDBIsLeftAlone(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LGIT_DB", filepath.Join(shared, "findings.db"))
	if _, err := defaultDBPath(); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, shared); m != 0o755 {
		t.Errorf("an existing directory chosen via LGIT_DB was chmod'ed to %v; it must be left alone", m)
	}
}

func TestStoreDir_DefaultDirectoryIsTightened(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LGIT_DB", "")
	def := filepath.Join(home, ".l0-git")
	if err := os.Mkdir(def, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := defaultDBPath(); err != nil {
		t.Fatal(err)
	}
	if m := modeOf(t, def); m != 0o700 {
		t.Errorf("default dir mode = %v, want 0700", m)
	}
}
