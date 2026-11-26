package s3

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/angels-you/bee/pkg/storage"
	"github.com/angels-you/bee/pkg/storage/s3/provider"
)

// TestPrepare — дефолты провайдера и отказ неполных creds
func TestPrepare(t *testing.T) {

	// hetzner: endpoint по региону, https
	c := Creds{Provider: provider.Hetzner, Key: "k", Secret: "s", Bucket: "b"}
	err := c.prepare()
	switch {
	case err != nil:
		t.Fatal(err)
	case c.Endpoint != "hel1.your-objectstorage.com":
		t.Errorf("endpoint %s", c.Endpoint)
	case !c.Secure:
		t.Error("cloud provider must be https")
	case c.Timeout != 10*time.Second:
		t.Errorf("timeout %s", c.Timeout)
	}

	// r2: регион всегда auto, endpoint по аккаунту
	c = Creds{Provider: provider.Cloudflare, Region: "eu", Account: "acc", Key: "k", Secret: "s", Bucket: "b"}
	err = c.prepare()
	switch {
	case err != nil:
		t.Fatal(err)
	case c.Region != "auto":
		t.Errorf("region %s", c.Region)
	case c.Endpoint != "acc.r2.cloudflarestorage.com":
		t.Errorf("endpoint %s", c.Endpoint)
	}

	// minio: свой endpoint, http можно
	c = Creds{Provider: provider.MinIO, Endpoint: "minio:9000", Key: "k", Secret: "s", Bucket: "b"}
	err = c.prepare()
	switch {
	case err != nil:
		t.Fatal(err)
	case c.Secure:
		t.Error("hosted provider keeps Secure as set")
	}

	// отказы
	bad := []Creds{
		{Key: "k", Secret: "s", Bucket: "b"},
		{Provider: provider.Cloudflare, Key: "k", Secret: "s", Bucket: "b"},
		{Provider: provider.IDrive, Key: "k", Secret: "s", Bucket: "b"},
		{Provider: provider.Hetzner, Secret: "s", Bucket: "b"},
		{Provider: provider.Hetzner, Key: "k", Bucket: "b"},
		{Provider: provider.Hetzner, Key: "k", Secret: "s"},
	}
	for i, x := range bad {
		err = x.prepare()
		if err == nil {
			t.Errorf("%d: %+v must be refused", i, x)
		}
	}
}

// TestBucketError — ошибка конфига возвращается из каждого метода
func TestBucketError(t *testing.T) {
	b := NewBucket(Creds{Provider: provider.Hetzner})

	err := b.Ping()
	if err == nil {
		t.Error("ping must fail")
	}

	_, err = b.Upload("a.txt", []byte("x"))
	if err == nil {
		t.Error("upload must fail")
	}

	_, err = New(Creds{Provider: provider.Hetzner}, "")
	if err == nil {
		t.Error("new must fail")
	}
}

// TestKeys — префикс хранилища, префикс листинга и срок подписи
func TestKeys(t *testing.T) {
	b := NewBucket(Creds{Provider: provider.Hetzner, Key: "k", Secret: "s", Bucket: "b"})

	st := NewWith(b, "/env/")
	if st.key("/a/../b.m3u8") != "env/b.m3u8" {
		t.Errorf("key %s", st.key("/a/../b.m3u8"))
	}

	root := NewWith(b, "")
	if root.key("a/b") != "a/b" {
		t.Errorf("root key %s", root.key("a/b"))
	}

	if b.prefix("v1/dir/*") != "v1/dir/" {
		t.Errorf("prefix %s", b.prefix("v1/dir/*"))
	}

	switch {
	case b.ttl(0) != 7*24*time.Hour:
		t.Error("ttl 0")
	case b.ttl(30*24*time.Hour) != 7*24*time.Hour:
		t.Error("ttl above max")
	case b.ttl(time.Millisecond) != time.Second:
		t.Error("ttl below a second")
	case b.ttl(time.Hour) != time.Hour:
		t.Error("ttl hour")
	}

	if b.Link("a") != "" {
		t.Error("no link without domain")
	}
}

// TestLive — живой тест бакета: только если заданы BEE_TEST_S3_PROVIDER,
// _KEY, _SECRET, _BUCKET (и при необходимости _ENDPOINT, _REGION,
// _ACCOUNT). Все объекты под test/, в конце DeletePrefix их убирает
func TestLive(t *testing.T) {
	name := os.Getenv("BEE_TEST_S3_PROVIDER")
	if name == "" {
		t.Skip("BEE_TEST_S3_PROVIDER is not set")
	}

	creds := Creds{
		Provider: provider.Provider(provider.ProviderIndex(name)),
		Endpoint: os.Getenv("BEE_TEST_S3_ENDPOINT"),
		Region:   os.Getenv("BEE_TEST_S3_REGION"),
		Account:  os.Getenv("BEE_TEST_S3_ACCOUNT"),
		Key:      os.Getenv("BEE_TEST_S3_KEY"),
		Secret:   os.Getenv("BEE_TEST_S3_SECRET"),
		Bucket:   os.Getenv("BEE_TEST_S3_BUCKET"),
	}

	// хранилище
	st, err := New(creds, "test")
	if err != nil {
		t.Fatal("open:", err)
	}

	var iface storage.Storage = st
	ctx := context.Background()
	body := fmt.Appendf(nil, "bee s3 test %s", time.Now().Format(time.RFC3339Nano))

	// запись
	err = iface.Put(ctx, "a/hello.txt", bytes.NewReader(body), int64(len(body)), "text/plain")
	if err != nil {
		t.Fatal("put:", err)
	}

	// есть
	ok, err := iface.Exists(ctx, "a/hello.txt")
	if err != nil || !ok {
		t.Fatalf("exists %v %v", ok, err)
	}

	// нет
	ok, err = iface.Exists(ctx, "a/nope.txt")
	if err != nil || ok {
		t.Fatalf("missing %v %v", ok, err)
	}

	// чтение
	b := st.bucket
	got, err := b.Get("test/a/hello.txt")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("get %q %v", got, err)
	}

	// метаданные
	info, err := b.Stat("test/a/hello.txt")
	switch {
	case err != nil:
		t.Fatal("stat:", err)
	case info.Size != int64(len(body)):
		t.Errorf("stat size %d", info.Size)
	case !strings.HasPrefix(info.ContentType, "text/plain"):
		t.Errorf("stat content type %s", info.ContentType)
	}

	// копия и перенос
	err = b.Copy("test/a/hello.txt", "test/b/copy.txt")
	if err != nil {
		t.Fatal("copy:", err)
	}

	err = b.Move("test/b/copy.txt", "test/b/moved.txt")
	if err != nil {
		t.Fatal("move:", err)
	}

	// листинг
	files, err := b.List("test/", 0)
	if err != nil || len(files) != 2 {
		t.Fatalf("list %d %v", len(files), err)
	}

	// подпись
	link, err := b.PresignRead("test/a/hello.txt", time.Minute)
	if err != nil || link == "" {
		t.Fatalf("presign %q %v", link, err)
	}

	// уборка
	deleted, err := b.DeletePrefix("test/")
	if err != nil || deleted != 2 {
		t.Fatalf("delete prefix %d %v", deleted, err)
	}
}
