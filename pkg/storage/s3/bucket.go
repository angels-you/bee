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

// Bucket is one bucket of any provider. With Creds.Domain Upload
// returns a public link, without it the object key. A config error
// is not lost: any method returns it, Ping checks it
type Bucket struct {
	creds Creds
	conn  *minio.Client
	err   error // config or connection error

	presignMaxTTL time.Duration // an s3 v4 signature lives at most 7 days
}

// NewBucket returns a bucket for creds; Ping checks the connection
func NewBucket(creds Creds) *Bucket {
	a := &Bucket{creds: creds}
	a.open()

	return a
}

// open prepares creds and connects; the error stays in a.err and
// is returned from every method (and from Ping)
func (a *Bucket) open() {
	a.presignMaxTTL = 7 * 24 * time.Hour

	// creds and provider defaults
	a.err = a.creds.prepare()
	if a.err != nil {
		return
	}

	// transport
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

	// client
	options := &minio.Options{
		Creds:     credentials.NewStaticV4(a.creds.Key, a.creds.Secret, ""),
		Secure:    a.creds.Secure,
		Region:    a.creds.Region,
		Transport: transport,
	}
	a.conn, a.err = minio.New(a.creds.Endpoint, options)
}

// dial uses ipv4 only
func (a *Bucket) dial(ctx context.Context, network, address string) (net.Conn, error) {
	d := net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	return d.DialContext(ctx, "tcp4", address)
}

// Name returns the bucket name
func (a *Bucket) Name() string {
	return a.creds.Bucket
}

// Provider returns the bucket provider
func (a *Bucket) Provider() provider.Provider {
	return a.creds.Provider
}

// Upload uploads from memory, see UploadReader
func (a *Bucket) Upload(name string, body []byte, meta ...Meta) (link string, err error) {
	return a.UploadReader(name, bytes.NewReader(body), int64(len(body)), meta...)
}

// UploadFile uploads local file from to to, see UploadReader
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

// UploadReader streams an upload under key name. size is -1 if the size
// is unknown: then multipart in parts. Content-Type by extension if
// not set in meta. With Domain returns a link, without it the key
func (a *Bucket) UploadReader(name string, body io.Reader, size int64, meta ...Meta) (link string, err error) {
	err = a.err
	if err != nil {
		return
	}

	// key
	key := tools.CleanKey(name)
	if key == "" {
		return "", errors.New("s3: object name is required")
	}

	// object headers
	var options Meta
	if len(meta) > 0 {
		options = meta[0]
	}
	if options.ContentType == "" {
		options.ContentType = mime.TypeByExtension(path.Ext(key))
	}

	c, cancel := a.context()
	defer cancel()

	// upload
	_, err = a.conn.PutObject(c, a.creds.Bucket, key, body, size, options)
	if err != nil {
		return
	}

	if a.creds.Domain == "" {
		return key, nil
	}

	return a.Link(key), nil
}

// Link returns a permanent link to an object by key; empty without Domain
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

// Get reads a whole object into memory
func (a *Bucket) Get(key string) (body []byte, err error) {
	o, err := a.GetReader(key)
	if err != nil {
		return
	}
	defer o.Close()

	return io.ReadAll(o)
}

// GetLink reads an object by its public link
func (a *Bucket) GetLink(link string) (body []byte, err error) {
	u, err := url.Parse(link)
	if err != nil {
		return
	}

	return a.Get(u.Path)
}

// GetReader streams an object; it must be closed. A "no object" error
// comes immediately, not on the first read
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

	// GetObject is lazy: the request is sent on the first read or Stat
	_, err = o.Stat()
	if err != nil {
		o.Close()
		return nil, err
	}

	return o, nil
}

// Stat returns object metadata without the body
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

// Exists reports whether the object exists. Absence is false without an error, an error only
// on a request failure
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

// List returns objects whose key starts with prefix (recursively). limit <= 0
// means all. "v1/file1" finds v1/file1, v1/file1.jpg, v1/file1/a.m3u8;
// "v1/dir/" only the folder contents
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

// Copy copies an object inside the bucket
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

// Move copies and deletes the source (s3 has no rename)
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

// Delete deletes an object; a missing key is not an error
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

// DeleteLink deletes an object by its public link
func (a *Bucket) DeleteLink(link string) (err error) {
	u, err := url.Parse(link)
	if err != nil {
		return
	}

	return a.Delete(u.Path)
}

// DeleteMany deletes a list of objects in batches (DeleteObjects, up to 1000 per
// request). Missing keys are not an error
func (a *Bucket) DeleteMany(keys ...string) (deleted int, err error) {
	err = a.err
	if err != nil {
		return
	}

	// key channel
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

// DeletePrefix deletes all objects whose key starts with prefix:
// "v1/file1" and "v1/file1*" delete v1/file1, v1/file1.jpg, v1/file1/hls/0.ts;
// "v1/dir/" deletes everything inside folder v1/dir. An empty prefix is forbidden: the whole
// bucket with one typo
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

	// listing and deletion run in parallel: keys do not pile up in memory
	var listed int
	var listErr error
	objects := make(chan minio.ObjectInfo)
	go a.listTo(ctx, prefix, objects, &listed, &listErr)

	// remove drains the channel until it is closed, so after it listTo has already
	// finished and listed / listErr can be read
	failed, err := a.remove(ctx, objects)
	if err == nil {
		err = listErr
	}

	return listed - failed, err
}

// PresignUpload returns a signed upload link (PUT). ttl <= 0 means the maximum
// signature lifetime (7 days)
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

// PresignRead returns a signed link to read an object. ttl <= 0 means the maximum
// signature lifetime (7 days)
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

// Ping checks access to its own bucket. ListBuckets does not fit: an r2 key
// issued for one bucket cannot see the account bucket list
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

// SetCORS sets the bucket CORS rule: browsers from origins may use methods (by
// default PUT, GET, HEAD) with any headers, ETag is visible in the response.
// Replaces the whole bucket CORS configuration
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

// context returns a context with the single request timeout from Creds.Timeout
func (a *Bucket) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), a.creds.Timeout)
}

// prefix returns the key prefix: without a trailing "*", but a trailing "/" is kept
// so "v1/dir/" does not touch v1/dir2
func (a *Bucket) prefix(prefix string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "*")
	dir := strings.HasSuffix(prefix, "/") || strings.HasSuffix(prefix, "\\")

	prefix = tools.CleanKey(prefix)
	if prefix != "" && dir {
		prefix += "/"
	}

	return prefix
}

// ttl returns the s3 v4 signature lifetime: from a second to 7 days, <= 0 means the maximum
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

// listTo sends keys with the prefix to the objects channel (closes it at the end);
// listed is how many were sent, listErr is the listing error
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

// remove runs DeleteObjects over the key channel; failed is how many keys could not
// be deleted, err is the first error
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

// file builds a File from an s3 response
func (a *Bucket) file(o minio.ObjectInfo) File {
	return File{
		Path:        o.Key,
		Size:        o.Size,
		ContentType: o.ContentType,
		ETag:        o.ETag,
		Modified:    o.LastModified,
	}
}
