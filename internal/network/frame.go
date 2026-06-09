package network

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const (
	FrameHeaderSize = 4
	DefaultMaxFrame = 1024 * 1024
)

type deadlineReader interface {
	io.Reader
	SetReadDeadline(time.Time) error
}

type deadlineWriter interface {
	io.Writer
	SetWriteDeadline(time.Time) error
}

// FrameOptions controls length limits and deadline behavior for frame IO.
type FrameOptions struct {
	MaxFrameSize uint32
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (o FrameOptions) maxFrameSize() uint32 {
	if o.MaxFrameSize == 0 {
		return DefaultMaxFrame
	}
	return o.MaxFrameSize
}

// ReadFrame reads one length-prefixed frame. The length prefix is a
// 4-byte unsigned integer in network byte order and excludes the header.
func ReadFrame(r io.Reader, maxFrameSize uint32) ([]byte, error) {
	return ReadFrameWithOptions(r, FrameOptions{MaxFrameSize: maxFrameSize})
}

// ReadFrameWithOptions reads one frame and applies a read deadline when the
// reader supports it.
func ReadFrameWithOptions(r io.Reader, opts FrameOptions) ([]byte, error) {
	maxFrameSize := opts.maxFrameSize()
	if opts.ReadTimeout > 0 {
		if dr, ok := r.(deadlineReader); ok {
			if err := dr.SetReadDeadline(time.Now().Add(opts.ReadTimeout)); err != nil {
				return nil, err
			}
		}
	}

	var header [FrameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, fmt.Errorf("read frame header: %w", err)
	}

	frameLen := binary.BigEndian.Uint32(header[:])
	if frameLen == 0 || frameLen > maxFrameSize {
		return nil, fmt.Errorf("invalid frame length: %d", frameLen)
	}

	frame := make([]byte, frameLen)
	if _, err := io.ReadFull(r, frame); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}

	return frame, nil
}

// WriteFrame writes one length-prefixed frame. It keeps writing until the
// whole frame is flushed or an error occurs.
func WriteFrame(w io.Writer, payload []byte, maxFrameSize uint32) error {
	return WriteFrameWithOptions(w, payload, FrameOptions{MaxFrameSize: maxFrameSize})
}

// WriteFrameWithOptions writes one frame and applies a write deadline when the
// writer supports it.
func WriteFrameWithOptions(w io.Writer, payload []byte, opts FrameOptions) error {
	maxFrameSize := opts.maxFrameSize()
	if len(payload) == 0 || uint32(len(payload)) > maxFrameSize {
		return fmt.Errorf("invalid frame length: %d", len(payload))
	}
	if opts.WriteTimeout > 0 {
		if dw, ok := w.(deadlineWriter); ok {
			if err := dw.SetWriteDeadline(time.Now().Add(opts.WriteTimeout)); err != nil {
				return err
			}
		}
	}

	var header [FrameHeaderSize]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))

	if err := writeFull(w, header[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if err := writeFull(w, payload); err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	return nil
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
