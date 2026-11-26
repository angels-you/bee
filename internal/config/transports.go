package config

import (
	"errors"
	"time"
)

// HTTP — приём job по http, health и metrics; пустой Addr — выключен
type HTTP struct {
	Addr string `env:"ADDR"` // :8080
}

// Enabled — транспорт включён
func (a *HTTP) Enabled() bool {
	return a.Addr != ""
}

// GRPC — приём job по grpc; пустой Addr — выключен
type GRPC struct {
	Addr string `env:"ADDR"` // :9090
}

// Enabled — транспорт включён
func (a *GRPC) Enabled() bool {
	return a.Addr != ""
}

// NATS — приём job из NATS JetStream; пустой URL — выключен. Name — имя
// stream, subjects и consumer этого bee: у bee с разными бакетами на одном
// nats имена должны быть разными
type NATS struct {
	URL  string `env:"URL"`  // nats://host:4222, учётные данные внутри url
	Name string `env:"NAME"` // stream BEE, subjects bee.audio.new и т.д., consumer bee

	// AckWait — сколько доставка живёт без признаков жизни, прежде чем
	// JetStream отдаст её другому bee; работающая job шлёт прогресс каждые
	// AckWait / 3
	AckWait time.Duration `env:"ACK_WAIT"`

	// MaxDeliver — доставок одной job: ошибка последней окончательна
	MaxDeliver    int           `env:"MAX_DELIVER"`
	RetryDelay    time.Duration `env:"RETRY_DELAY"`     // задержка nak после первой доставки, дальше удваивается
	RetryMaxDelay time.Duration `env:"RETRY_MAX_DELAY"` // самая длинная задержка nak

	// stream и журнал результатов: work queue, сообщение живёт до ack
	Storage  string        `env:"STORAGE"`  // file или memory
	Replicas int           `env:"REPLICAS"` // 1..5, не больше серверов кластера
	MaxAge   time.Duration `env:"MAX_AGE"`  // неразобранное сообщение удаляется через это время; 0 — никогда

	// ResultTTL — сколько журнал хранит результат готовой job: дольше, чем
	// job может ждать передоставку
	ResultTTL time.Duration `env:"RESULT_TTL"`
}

// Enabled — транспорт включён
func (a *NATS) Enabled() bool {
	return a.URL != ""
}

// defaults — значения из рабочих reels и gin
func (a *NATS) defaults() {
	a.Name = "bee"
	a.AckWait = time.Minute
	a.MaxDeliver = 5
	a.RetryDelay = 10 * time.Second
	a.RetryMaxDelay = 5 * time.Minute
	a.Storage = "file"
	a.Replicas = 1
	a.MaxAge = 0
	a.ResultTTL = 24 * time.Hour
}

// validate — поля включённого nats в своих пределах
func (a *NATS) validate() (errs []error) {
	if a.Name == "" {
		errs = append(errs, errors.New("config: BEE_NATS_NAME is required"))
	}

	if a.AckWait <= 0 {
		errs = append(errs, errors.New("config: BEE_NATS_ACK_WAIT must be positive"))
	}

	if a.MaxDeliver < 1 {
		errs = append(errs, errors.New("config: BEE_NATS_MAX_DELIVER must be at least 1"))
	}

	if a.RetryDelay <= 0 {
		errs = append(errs, errors.New("config: BEE_NATS_RETRY_DELAY must be positive"))
	}

	if a.RetryMaxDelay < a.RetryDelay {
		errs = append(errs, errors.New("config: BEE_NATS_RETRY_MAX_DELAY must be at least BEE_NATS_RETRY_DELAY"))
	}

	switch a.Storage {
	case "file", "memory":
	default:
		errs = append(errs, errors.New("config: BEE_NATS_STORAGE must be file or memory"))
	}

	if !(a.Replicas >= 1 && a.Replicas <= 5) {
		errs = append(errs, errors.New("config: BEE_NATS_REPLICAS must be within 1..5"))
	}

	if a.MaxAge < 0 {
		errs = append(errs, errors.New("config: BEE_NATS_MAX_AGE must not be negative"))
	}

	if a.ResultTTL <= 0 {
		errs = append(errs, errors.New("config: BEE_NATS_RESULT_TTL must be positive"))
	}

	return
}
