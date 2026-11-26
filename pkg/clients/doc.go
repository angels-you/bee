// Package clients holds the Go clients of bee, one per transport, with
// the same API: Audio, Stream, Video and Images take a job of
// github.com/angels-you/bee/pkg/types and return its result.
//
//	pkg/clients/http   HTTP/JSON, token required
//	pkg/clients/grpc   gRPC, token required
//	pkg/clients/nats   NATS JetStream, the queue survives a restart of bee
//
// A failed job returns its result with Error set and the same
// *code.Error as the error (pkg/types/code): Error.Retryable tells
// whether to try again.
package clients
