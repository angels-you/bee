package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/angels-you/bee/pkg/storage/s3/provider"
	"github.com/angels-you/bee/pkg/tools"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Bucket — один бакет любого провайдера. С Creds.Domain Upload
// возвращает публичную ссылку, без него — ключ объекта. Ошибка конфига
// не теряется: её вернёт любой метод, проверка — Ping
type Bucket struct {
	creds Creds
	conn  *minio.Client
	err   error // ошибка конфига или подключения

	presignMaxTTL time.Duration // подпись s3 v4 живёт максимум 7 дней
}

// NewBucket — бакет по creds; подключение проверяет Ping
func NewBucket(creds Creds) *Bucket {
	a := &Bucket{creds: creds}
	a.open()

	return a
}

// open — подготовка creds и подключение; ошибка остаётся в a.err и
// возвращается из каждого метода (и из Ping)
func (a *Bucket) open() {
	a.presignMaxTTL = 7 * 24 * time.Hour

	// creds и дефолты провайдера
	a.err = a.creds.prepare()
	if a.err != nil {
		return
	}

	// транспорт
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           a.dial,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    true,
	}

	// клиент
	options := &minio.Options{
		Creds:     credentials.NewStaticV4(a.creds.Key, a.creds.Secret, ""),
		Secure:    a.creds.Secure,
		Region:    a.creds.Region,
		Transport: transport,
	}
	a.conn, a.err = minio.New(a.creds.Endpoint, options)
}

// dial — только ipv4
func (a *Bucket) dial(ctx context.Context, network, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	return d.DialContext(ctx, "tcp4", address)
}

// Name — имя бакета
func (a *Bucket) Name() string {
	return a.creds.Bucket
}

// Provider — провайдер бакета
func (a *Bucket) Provider() provider.Provider {
	return a.creds.Provider
}

// Upload — загрузка из памяти, см. UploadReader
func (a *Bucket) Upload(name string, body []byte, meta ...Meta) (link string, err error) {
	return a.UploadReader(name, bytes.NewReader(body), int64(len(body)), meta...)
}

// UploadFile — загрузка локального файла from в to, см. UploadReader
func (a *Bucket) UploadFile(from, to string, meta ...Meta) (link string, err error) {
	f, err := os.Open(from)
	if err != nil {
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return
	}

	return a.UploadReader(to, f, info.Size(), meta...)
}

// UploadReader — загрузка потоком под ключ name. size -1, если размер
// неизвестен: тогда multipart частями. Content-Type по расширению, если
// не задан в meta. С Domain возвращает ссылку, без него — ключ
func (a *Bucket) UploadReader(name string, body io.Reader, size int64, meta ...Meta) (link string, err error) {
	err = a.err
	if err != nil {
		return
	}

	// ключ
	key := tools.CleanKey(name)
	if key == "" {
		return "", errors.New("s3: object name is required")
	}

	// заголовки объекта
	var options Meta
	if len(meta) > 0 {
		options = meta[0]
	}
	if options.ContentType == "" {
		options.ContentType = mime.TypeByExtension(path.Ext(key))
	}

	c, cancel := a.context()
	defer cancel()

	// загрузка
	_, err = a.conn.PutObject(c, a.creds.Bucket, key, body, size, options)
	if err != nil {
		return
	}

	if a.creds.Domain == "" {
		return key, nil
	}

	return a.Link(key), nil
}

// Link — постоянная ссылка на объект по ключу; пусто без Domain
func (a *Bucket) Link(key string) (link string) {
	key = tools.CleanKey(key)

	switch {
	case key == "":
		return ""
	case a.creds.Domain == "":
		return ""
	}

	return a.creds.Domain + "/" + key
}

// Get — объект целиком в память
func (a *Bucket) Get(key string) (body []byte, err error) {
	o, err := a.GetReader(key)
	if err != nil {
		return
	}
	defer o.Close()

	return io.ReadAll(o)
}

// GetLink — объект по публичной ссылке
func (a *Bucket) GetLink(link string) (body []byte, err error) {
	u, err := url.Parse(link)
	if err != nil {
		return
	}

	return a.Get(u.Path)
}

// GetReader — объект потоком; закрыть обязательно. Ошибка «нет объекта»
// приходит сразу, а не при первом чтении
func (a *Bucket) GetReader(key string) (body io.ReadCloser, err error) {
	err = a.err
	if err != nil {
		return
	}

	key = tools.CleanKey(key)
	if key == "" {
		return nil, errors.New("s3: object name is required")
	}

	o, err := a.conn.GetObject(context.Background(), a.creds.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return
	}

	// GetObject ленивый: запрос уходит на первом чтении или Stat
	_, err = o.Stat()
	if err != nil {
		o.Close()
		return nil, err
	}

	return o, nil
}

// Stat — метаданные объекта без тела
func (a *Bucket) Stat(key string) (res File, err error) {
	err = a.err
	if err != nil {
		return
	}

	key = tools.CleanKey(key)
	if key == "" {
		return res, errors.New("s3: object name is required")
	}

	c, cancel := a.context()
	defer cancel()

	info, err := a.conn.StatObject(c, a.creds.Bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return
	}

	return a.file(info), nil
}

// Exists — есть ли объект. Отсутствие — false без ошибки, ошибка только
// при сбое запроса
func (a *Bucket) Exists(key string) (ok bool, err error) {
	_, err = a.Stat(key)
	if err == nil {
		return true, nil
	}

	r := minio.ToErrorResponse(err)
	switch {
	case r.Code == "NoSuchKey":
		return false, nil
	case r.StatusCode == http.StatusNotFound:
		return false, nil
	}

	return false, err
}

// List — объекты с ключом, начинающимся с prefix (рекурсивно). limit <= 0
// — все. "v1/file1" найдёт v1/file1, v1/file1.jpg, v1/file1/a.m3u8;
// "v1/dir/" — только содержимое папки
func (a *Bucket) List(prefix string, limit int) (res []File, err error) {
	err = a.err
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	options := minio.ListObjectsOptions{
		Prefix:    a.prefix(prefix),
		Recursive: true,
	}
	for o := range a.conn.ListObjects(ctx, a.creds.Bucket, options) {
		if o.Err != nil {
			return res, o.Err
		}

		res = append(res, a.file(o))
		if limit > 0 && len(res) >= limit {
			return
		}
	}

	return
}

// Copy — копия объекта внутри бакета
func (a *Bucket) Copy(from, to string) (err error) {
	err = a.err
	if err != nil {
		return
	}

	from = tools.CleanKey(from)
	to = tools.CleanKey(to)

	switch {
	case from == "":
		return errors.New("s3: object name is required")
	case to == "":
		return errors.New("s3: object name is required")
	case from == to:
		return nil
	}

	c, cancel := a.context()
	defer cancel()

	dst := minio.CopyDestOptions{Bucket: a.creds.Bucket, Object: to}
	src := minio.CopySrcOptions{Bucket: a.creds.Bucket, Object: from}
	_, err = a.conn.CopyObject(c, dst, src)
	return
}

// Move — копия и удаление исходника (в s3 нет переименования)
func (a *Bucket) Move(from, to string) (err error) {
	if tools.CleanKey(from) == tools.CleanKey(to) {
		return a.err
	}

	err = a.Copy(from, to)
	if err != nil {
		return
	}

	return a.Delete(from)
}

// Delete — удалить объект; отсутствующий ключ не ошибка
func (a *Bucket) Delete(key string) (err error) {
	err = a.err
	if err != nil {
		return
	}

	key = tools.CleanKey(key)
	if key == "" {
		return errors.New("s3: object name is required")
	}

	c, cancel := a.context()
	defer cancel()

	return a.conn.RemoveObject(c, a.creds.Bucket, key, minio.RemoveObjectOptions{})
}

// DeleteLink — удалить объект по публичной ссылке
func (a *Bucket) DeleteLink(link string) (err error) {
	u, err := url.Parse(link)
	if err != nil {
		return
	}

	return a.Delete(u.Path)
}

// DeleteMany — удалить список объектов пачками (DeleteObjects, до 1000 за
// запрос). Отсутствующие ключи не ошибка
func (a *Bucket) DeleteMany(keys ...string) (deleted int, err error) {
	err = a.err
	if err != nil {
		return
	}

	// канал ключей
	objects := make(chan minio.ObjectInfo, len(keys))
	for _, x := range keys {
		x = tools.CleanKey(x)
		if x == "" {
			continue
		}

		objects <- minio.ObjectInfo{Key: x}
		deleted++
	}
	close(objects)

	failed, err := a.remove(context.Background(), objects)
	return deleted - failed, err
}

// DeletePrefix — удалить все объекты, чей ключ начинается с prefix:
// "v1/file1" и "v1/file1*" удалят v1/file1, v1/file1.jpg, v1/file1/hls/0.ts;
// "v1/dir/" — всё внутри папки v1/dir. Пустой префикс запрещён: весь
// бакет одной опечаткой
func (a *Bucket) DeletePrefix(prefix string) (deleted int, err error) {
	err = a.err
	if err != nil {
		return
	}

	prefix = a.prefix(prefix)
	if prefix == "" {
		return 0, errors.New("s3: prefix is required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// листинг и удаление идут параллельно: ключи не копятся в памяти
	var listed int
	var listErr error
	objects := make(chan minio.ObjectInfo)
	go a.listTo(ctx, prefix, objects, &listed, &listErr)

	// remove дочитывает канал до закрытия, поэтому после него listTo уже
	// завершился и listed / listErr можно читать
	failed, err := a.remove(ctx, objects)
	if err == nil {
		err = listErr
	}

	return listed - failed, err
}

// PresignUpload — подписанная ссылка на загрузку (PUT). ttl <= 0 — максимум
// подписи (7 дней)
func (a *Bucket) PresignUpload(key string, ttl time.Duration) (link string, err error) {
	err = a.err
	if err != nil {
		return
	}

	key = tools.CleanKey(key)
	if key == "" {
		return "", errors.New("s3: object name is required")
	}

	u, err := a.conn.PresignedPutObject(context.Background(), a.creds.Bucket, key, a.ttl(ttl))
	if err != nil {
		return
	}

	if u == nil {
		return "", errors.New("s3: presign link is empty")
	}

	return u.String(), nil
}

// PresignRead — подписанная ссылка на чтение объекта. ttl <= 0 — максимум
// подписи (7 дней)
func (a *Bucket) PresignRead(key string, ttl time.Duration) (link string, err error) {
	err = a.err
	if err != nil {
		return
	}

	key = tools.CleanKey(key)
	if key == "" {
		return "", errors.New("s3: object name is required")
	}

	u, err := a.conn.PresignedGetObject(context.Background(), a.creds.Bucket, key, a.ttl(ttl), nil)
	if err != nil {
		return
	}

	if u == nil {
		return "", errors.New("s3: presign link is empty")
	}

	return u.String(), nil
}

// Ping — доступ к своему бакету. ListBuckets не годится: ключ r2,
// выданный на один бакет, список бакетов аккаунта не видит
func (a *Bucket) Ping() (err error) {
	err = a.err
	if err != nil {
		return
	}

	c, cancel := a.context()
	defer cancel()

	ok, err := a.conn.BucketExists(c, a.creds.Bucket)
	if err != nil {
		return
	}

	if !ok {
		return errors.New("s3: bucket not found")
	}

	return nil
}

// SetCORS — правило CORS бакета: браузеру с origins разрешены methods (по
// умолчанию PUT, GET, HEAD) с любыми заголовками, в ответе виден ETag.
// Заменяет всю конфигурацию CORS бакета
func (a *Bucket) SetCORS(origins []string, methods ...string) (err error) {
	err = a.err
	if err != nil {
		return
	}

	if len(origins) == 0 {
		return errors.New("s3: cors origins are required")
	}

	if len(methods) == 0 {
		methods = []string{"PUT", "GET", "HEAD"}
	}

	c, cancel := a.context()
	defer cancel()

	rule := cors.Rule{
		AllowedOrigin: origins,
		AllowedMethod: methods,
		AllowedHeader: []string{"*"},
		ExposeHeader:  []string{"ETag"},
		MaxAgeSeconds: 3600,
	}
	return a.conn.SetBucketCors(c, a.creds.Bucket, cors.NewConfig([]cors.Rule{rule}))
}

// context — таймаут одного запроса из Creds.Timeout
func (a *Bucket) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), a.creds.Timeout)
}

// prefix — префикс ключа: без "*" в конце, но завершающий "/" сохраняется
// — "v1/dir/" не должен задеть v1/dir2
func (a *Bucket) prefix(prefix string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "*")
	dir := strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, "\\")

	prefix = tools.CleanKey(prefix)
	if prefix != "" && dir {
		prefix += "/"
	}

	return prefix
}

// ttl — срок подписи s3 v4: от секунды до 7 дней, <= 0 — максимум
func (a *Bucket) ttl(ttl time.Duration) time.Duration {
	switch {
	case ttl <= 0:
		return a.presignMaxTTL
	case ttl > a.presignMaxTTL:
		return a.presignMaxTTL
	case ttl < time.Second:
		return time.Second
	}

	return ttl
}

// listTo — ключи с префиксом в канал objects (закрывает его в конце);
// listed — сколько отдано, listErr — ошибка листинга
func (a *Bucket) listTo(ctx context.Context, prefix string, objects chan<- minio.ObjectInfo, listed *int, listErr *error) {
	defer close(objects)

	options := minio.ListObjectsOptions{Prefix: prefix, Recursive: true}
	for o := range a.conn.ListObjects(ctx, a.creds.Bucket, options) {
		if o.Err != nil {
			*listErr = o.Err
			return
		}

		select {
		case objects <- o:
			*listed++
		case <-ctx.Done():
			return
		}
	}
}

// remove — DeleteObjects по каналу ключей; failed — сколько ключей не
// удалось удалить, err — первая из ошибок
func (a *Bucket) remove(ctx context.Context, objects <-chan minio.ObjectInfo) (failed int, err error) {
	for e := range a.conn.RemoveObjects(ctx, a.creds.Bucket, objects, minio.RemoveObjectsOptions{}) {
		if e.Err == nil {
			continue
		}

		failed++
		if err == nil {
			err = e.Err
		}
	}

	return
}

// file — File из ответа s3
func (a *Bucket) file(o minio.ObjectInfo) File {
	return File{
		Path:        o.Key,
		Size:        o.Size,
		ContentType: o.ContentType,
		ETag:        o.ETag,
		Modified:    o.LastModified,
	}
}
