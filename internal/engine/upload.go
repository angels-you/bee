package engine

import (
	"bytes"
	"context"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/angels-you/bee/internal/encrypt"
	"github.com/angels-you/bee/pkg/types/code"
)

// upload — один объект для публикации: локальный файл или байты из памяти
type upload struct {
	key     string
	file    string       // локальный файл; пусто, если задан data
	data    []byte       // сделано в памяти (playlist, json)
	encrypt *encrypt.HLS // сегмент HLS job с ключом: шифруется в памяти прямо перед Put
	track   *track       // вариант HLS, чей это init или сегмент; nil у остальных
	mime    string       // Content-Type; пусто — по расширению ключа
}

// track — опубликованные файлы одного варианта HLS: размеры после
// шифрования и IV каждого зашифрованного сегмента, по имени файла
type track struct {
	sizes map[string]int64
	ivs   map[string][]byte
}

// newTrack — пустой вариант
func newTrack() *track {
	return &track{
		sizes: map[string]int64{},
		ivs:   map[string][]byte{},
	}
}

// putAll — загрузки одной волны, BEE_UPLOAD_CONCURRENCY одновременно;
// первая ошибка отменяет остальные
func (a *work) putAll(ctx context.Context, list []upload) *code.Error {
	t := time.Now()
	defer a.took(&a.uploadMs, t)

	wctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// волна
	a.failed = nil
	sem := make(chan struct{}, a.engine.cfg.UploadConcurrency)
	var wg sync.WaitGroup
	for _, u := range list {
		wg.Add(1)
		go a.putOne(wctx, cancel, u, sem, &wg)
	}
	wg.Wait()

	// итог волны
	err := a.failed
	if err == nil {
		err = ctx.Err()
	}

	if err == nil {
		return nil
	}

	return a.storageError(ctx, "upload", err)
}

// putOne — одна загрузка, когда свободен слот; ошибка записывается один
// раз и отменяет волну
func (a *work) putOne(ctx context.Context, cancel context.CancelFunc, u upload, sem chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}

	err := a.put(ctx, u)
	<-sem
	if err == nil {
		return
	}

	a.mu.Lock()
	if a.failed == nil && ctx.Err() == nil {
		a.failed = err
	}
	a.mu.Unlock()

	cancel()
}

// put — один объект с его Content-Type: сегмент job с ключом шифруется в
// памяти новым IV, байты из памяти как есть, файл — потоком с диска
func (a *work) put(ctx context.Context, u upload) error {
	switch {
	case u.encrypt != nil:
		return a.putEncrypted(ctx, u)
	case u.data != nil:
		return a.putBytes(ctx, u, u.data, nil)
	}

	return a.putFile(ctx, u)
}

// putEncrypted — сегмент целиком (сегменты маленькие), зашифрованный
// AES-128-CBC ключом job и новым случайным IV
func (a *work) putEncrypted(ctx context.Context, u upload) error {
	plain, err := os.ReadFile(u.file)
	if err != nil {
		return err
	}

	data, iv, err := u.encrypt.Segment(plain)
	if err != nil {
		return err
	}

	return a.putBytes(ctx, u, data, iv)
}

// putBytes — данные записаны; записан их размер (и IV шифрования)
func (a *work) putBytes(ctx context.Context, u upload, data, iv []byte) error {
	err := a.engine.storage.Put(ctx, u.key, bytes.NewReader(data), int64(len(data)), u.contentType())
	if err != nil {
		return err
	}

	a.record(u, int64(len(data)), iv)
	return nil
}

// putFile — локальный файл потоком в хранилище; записан его размер
func (a *work) putFile(ctx context.Context, u upload) error {
	f, err := os.Open(u.file)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	err = a.engine.storage.Put(ctx, u.key, f, info.Size(), u.contentType())
	if err != nil {
		return err
	}

	a.record(u, info.Size(), nil)
	return nil
}

// record — опубликованный объект: байты посчитаны; у файла HLS его размер
// и IV зашифрованного сегмента записаны в вариант под именем файла
func (a *work) record(u upload, size int64, iv []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.uploaded += size
	if u.track == nil {
		return
	}

	name := path.Base(u.key)
	u.track.sizes[name] = size
	if iv != nil {
		u.track.ivs[name] = iv
	}
}

// contentType — Content-Type объекта по расширению ключа
func (a upload) contentType() string {
	if a.mime != "" {
		return a.mime
	}

	switch strings.ToLower(path.Ext(a.key)) {
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".mp4":
		return "video/mp4"
	case ".m4s":
		return "video/iso.segment"
	case ".ts":
		return "video/mp2t"
	case ".json":
		return "application/json"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	}

	return "application/octet-stream"
}
