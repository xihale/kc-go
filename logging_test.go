package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotatingWriterRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	// maxBytes=50, backups=2：超过 50 字节即轮转，保留 .1 和 .2
	w, err := newRotatingWriter(path, 50, 2)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer w.Close()

	// 每条约 30 字节，写三条应触发至少一次轮转
	for i := 0; i < 3; i++ {
		if _, err := w.Write([]byte("line-" + string(rune('A'+i)) + "-padding-here!!\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	names := map[string]bool{}
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		names[f.Name()] = true
	}
	if !names["app.log"] {
		t.Errorf("current log missing; dir=%v", names)
	}
	if !names["app.log.1"] {
		t.Errorf("backup .1 missing; dir=%v", names)
	}
	if names["app.log.3"] {
		t.Errorf("backup .3 should not exist (backups=2); dir=%v", names)
	}

	cur, _ := os.ReadFile(path)
	if int64(len(cur)) > 50 {
		t.Errorf("current file %d bytes > maxBytes 50", len(cur))
	}
}
