package store

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteFileAtomicCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := WriteFileAtomic(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFileAtomic: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteFileAtomicOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := WriteFileAtomic(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("content = %q, want second", got)
	}
}

// A concurrent reader must see either the old document or the new one in full,
// never a truncated prefix. This is the property the waybar poll depends on.
func TestWriteFileAtomicNeverExposesPartialContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	old := bytes.Repeat([]byte("a"), 64*1024)
	new := bytes.Repeat([]byte("b"), 64*1024)
	if err := WriteFileAtomic(path, old, 0o600); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	bad := make(chan []byte, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			got, err := os.ReadFile(path)
			if err != nil {
				continue // rename windows can briefly surface ENOENT on some systems
			}
			if !bytes.Equal(got, old) && !bytes.Equal(got, new) {
				select {
				case bad <- got:
				default:
				}
				return
			}
		}
	}()

	for i := 0; i < 200; i++ {
		payload := new
		if i%2 == 0 {
			payload = old
		}
		if err := WriteFileAtomic(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()

	select {
	case got := <-bad:
		t.Fatalf("reader observed a partial document of %d bytes", len(got))
	default:
	}
}

func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	for i := 0; i < 5; i++ {
		if err := WriteFileAtomic(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only out.json", names)
	}
}
