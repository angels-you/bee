// Package proto — протокол bee, общий для серверов и клиентов: bee.proto
// и сгенерированный код gRPC, типы job (Kind) и имена транспортов
// (Names): пути http, trailers gRPC, stream и subjects nats.
//
// На всех транспортах job и результат — json публичных типов
// github.com/angels-you/bee/pkg/types/audio, video, images
package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Kind — тип job
type Kind string

const (
	Audio  Kind = "audio"  // audio.Job → audio.Result
	Stream Kind = "stream" // video.Stream → video.Result
	Video  Kind = "video"  // video.Video → video.Result
	Images Kind = "images" // images.Job → images.Result
)

// Kinds — все типы job
func Kinds() []Kind {
	return []Kind{Audio, Stream, Video, Images}
}

// Valid — тип известен
func (a Kind) Valid() bool {
	switch a {
	case Audio, Stream, Video, Images:
		return true
	}

	return false
}

// Names — имена транспортов одного bee. Name — BEE_NATS_NAME (пусто —
// "bee"): у bee с разными бакетами на одном nats имена разные
type Names struct {
	Name string
}

// name — имя bee, по умолчанию bee
func (a Names) name() string {
	if a.Name == "" {
		return "bee"
	}

	return a.Name
}

// Path — путь http job типа kind: POST /v1/<kind>
func (a Names) Path(kind Kind) string {
	return "/v1/" + string(kind)
}

// TrailerCode — trailer gRPC с кодом ошибки упавшей job
func (a Names) TrailerCode() string {
	return "bee-error-code"
}

// TrailerRetryable — trailer gRPC: можно ли повторить
func (a Names) TrailerRetryable() string {
	return "bee-retryable"
}

// Stream — stream JetStream job (work queue): BEE
func (a Names) Stream() string {
	return strings.ToUpper(a.name())
}

// Results — stream JetStream результатов: BEE_RESULTS; хранит результаты
// BEE_NATS_RESULT_TTL, их читает клиент
func (a Names) Results() string {
	return a.Stream() + "_RESULTS"
}

// Ledger — key-value JetStream журнала готовых job: BEE_LEDGER
func (a Names) Ledger() string {
	return a.Stream() + "_LEDGER"
}

// Durable — общий pull consumer job всех bee с этим именем
func (a Names) Durable() string {
	return a.name()
}

// Jobs — subject job типа kind: bee.audio.new
func (a Names) Jobs(kind Kind) string {
	return a.name() + "." + string(kind) + ".new"
}

// AllJobs — subjects job всех типов: bee.*.new
func (a Names) AllJobs() string {
	return a.name() + ".*.new"
}

// Done — subject результата job id типа kind:
// bee.audio.done.<sha256(id) hex, 32 знака>; id в subject не годится как
// есть (точки, пробелы, *, >)
func (a Names) Done(kind Kind, id string) string {
	sum := sha256.Sum256([]byte(id))

	return a.name() + "." + string(kind) + ".done." + hex.EncodeToString(sum[:16])
}

// AllDone — subjects результатов всех типов и job: bee.*.done.>
func (a Names) AllDone() string {
	return a.name() + ".*.done.>"
}

// MsgID — Nats-Msg-Id job: повторная публикация той же job в окне
// дедупликации JetStream не создаёт вторую
func (a Names) MsgID(kind Kind, id string) string {
	return string(kind) + ":" + id
}
