// Package ingest accepts tracker connections and passes decoded locations to
// the application boundary.
package ingest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mleczakm/herring/internal/protocol/sinotrack"
)

const (
	defaultMaxFrameSize = 4096
	defaultIdleTimeout  = 5 * time.Minute
)

// LocationHandler receives a validated tracker report. Implementations must be
// safe for concurrent calls and should return quickly.
type LocationHandler func(context.Context, sinotrack.Location) error

// Server is a bounded TCP listener for SinoTrack frames.
type Server struct {
	Addr         string
	MaxFrameSize int
	IdleTimeout  time.Duration
	Logger       *slog.Logger
	OnLocation   LocationHandler
}

// ListenAndServe runs until ctx is cancelled or the listener fails.
func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return fmt.Errorf("listen for trackers: %w", err)
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	s.logger().Info("tracker listener started", "address", listener.Addr().String())
	var connections sync.WaitGroup
	defer connections.Wait()

	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept tracker connection: %w", err)
		}
		connections.Add(1)
		go func() {
			defer connections.Done()
			s.handleConnection(ctx, connection)
		}()
	}
}

func (s *Server) handleConnection(ctx context.Context, connection net.Conn) {
	defer connection.Close()
	remoteAddress := connection.RemoteAddr().String()
	logger := s.logger().With("remote_address", remoteAddress)
	reader := bufio.NewReaderSize(connection, s.maxFrameSize()+1)

	for {
		if err := connection.SetDeadline(time.Now().Add(s.idleTimeout())); err != nil {
			logger.Warn("could not set tracker deadline", "error", err)
			return
		}
		frame, err := reader.ReadSlice('#')
		if len(frame) > s.maxFrameSize() || errors.Is(err, bufio.ErrBufferFull) {
			logger.Warn("tracker frame exceeds limit", "limit_bytes", s.maxFrameSize())
			return
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !isTimeout(err) && ctx.Err() == nil {
				logger.Warn("could not read tracker frame", "error", err)
			}
			return
		}

		location, err := sinotrack.ParseLocation(strings.TrimSpace(string(frame)))
		if err != nil {
			logger.Warn("rejected tracker frame", "error", err)
			continue
		}
		if s.OnLocation != nil {
			if err := s.OnLocation(ctx, location); err != nil {
				logger.Error("could not handle tracker location", "device_id", location.DeviceID, "error", err)
				continue
			}
		}

		if _, err := io.WriteString(connection, location.Acknowledgement(time.Now())); err != nil {
			logger.Warn("could not acknowledge tracker frame", "device_id", location.DeviceID, "error", err)
			return
		}
	}
}

func (s *Server) maxFrameSize() int {
	if s.MaxFrameSize > 0 {
		return s.MaxFrameSize
	}
	return defaultMaxFrameSize
}

func (s *Server) idleTimeout() time.Duration {
	if s.IdleTimeout > 0 {
		return s.IdleTimeout
	}
	return defaultIdleTimeout
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func isTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
