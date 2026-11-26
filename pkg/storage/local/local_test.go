package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/angels-you/bee/pkg/storage"
)

func TestLocal(t *testing.T) {
	dir := t.TempDir()
	local, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}

	// local реализует storage.Storage
	var st storage.Storage = local
	ctx := context.Background()

	// ключ чистится
	err = st.Put(ctx, "/a/b/../c.json", strings.NewReader(`{}`), 2, "application/json")
	if err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(filepath.Join(dir, "a", "c.json"))
	if err != nil {
		t.Fatal("key must be cleaned:", err)
	}

	// ключ за пределами папки остаётся внутри
	err = st.Put(ctx, "../../escape.json", strings.NewReader(`{}`), 2, "application/json")
	if err != nil {
		t.Fatal(err)
	}

	_, err = os.Stat(filepath.Join(dir, "escape.json"))
	if err != nil {
		t.Fatal("key must stay inside the dir:", err)
	}

	// объект есть
	ok, err := st.Exists(ctx, "a/c.json")
	if err != nil || !ok {
		t.Fatalf("exists %v %v", ok, err)
	}

	// содержимое
	b, err := os.ReadFile(filepath.Join(dir, "a", "c.json"))
	if err != nil || string(b) != "{}" {
		t.Fatalf("content %q %v", b, err)
	}

	// объекта нет
	ok, err = st.Exists(ctx, "nope")
	if err != nil || ok {
		t.Errorf("missing: %v %v", ok, err)
	}

	err = st.Ping(ctx)
	if err != nil {
		t.Error(err)
	}

	// пустая папка запрещена
	_, err = New("")
	if err == nil {
		t.Error("empty dir must be refused")
	}
}
