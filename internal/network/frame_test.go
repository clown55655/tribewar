package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type chunkedReader struct {
	data      []byte
	chunkSize int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := r.chunkSize
	if n <= 0 || n > len(r.data) {
		n = len(r.data)
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, r.data[:n])
	r.data = r.data[n:]
	return n, nil
}

type chunkedWriter struct {
	buf       bytes.Buffer
	chunkSize int
}

func (w *chunkedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := w.chunkSize
	if n <= 0 || n > len(p) {
		n = len(p)
	}
	w.buf.Write(p[:n])
	return n, nil
}

type deadlineReadBuffer struct {
	reader       *bytes.Reader
	readDeadline time.Time
}

func (r *deadlineReadBuffer) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *deadlineReadBuffer) SetReadDeadline(t time.Time) error {
	r.readDeadline = t
	return nil
}

type deadlineWriteBuffer struct {
	buf           bytes.Buffer
	writeDeadline time.Time
}

func (w *deadlineWriteBuffer) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *deadlineWriteBuffer) SetWriteDeadline(t time.Time) error {
	w.writeDeadline = t
	return nil
}

func encodeFrame(payload []byte) []byte {
	frame := make([]byte, FrameHeaderSize+len(payload))
	binary.BigEndian.PutUint32(frame[:FrameHeaderSize], uint32(len(payload)))
	copy(frame[FrameHeaderSize:], payload)
	return frame
}

func TestReadFrameHandlesPartialReads(t *testing.T) {
	want := []byte("hello framed stream")
	reader := &chunkedReader{
		data:      encodeFrame(want),
		chunkSize: 2,
	}

	got, err := ReadFrame(reader, DefaultMaxFrame)
	if err != nil {
		t.Fatalf("ReadFrame error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload mismatch: got %q want %q", got, want)
	}
}

func TestReadFrameRejectsInvalidLength(t *testing.T) {
	var header [FrameHeaderSize]byte
	binary.BigEndian.PutUint32(header[:], DefaultMaxFrame+1)

	_, err := ReadFrame(bytes.NewReader(header[:]), DefaultMaxFrame)
	if err == nil {
		t.Fatal("expected invalid length error")
	}
}

func TestReadFrameRejectsZeroLength(t *testing.T) {
	var header [FrameHeaderSize]byte

	_, err := ReadFrame(bytes.NewReader(header[:]), DefaultMaxFrame)
	if err == nil {
		t.Fatal("expected invalid length error")
	}
}

func TestReadFrameReturnsErrorForShortBody(t *testing.T) {
	frame := encodeFrame([]byte("hello"))
	reader := bytes.NewReader(frame[:len(frame)-2])

	_, err := ReadFrame(reader, DefaultMaxFrame)
	if err == nil {
		t.Fatal("expected short body error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected ErrUnexpectedEOF, got %v", err)
	}
}

func TestReadFrameAppliesReadDeadline(t *testing.T) {
	want := []byte("deadline payload")
	reader := &deadlineReadBuffer{
		reader: bytes.NewReader(encodeFrame(want)),
	}

	got, err := ReadFrameWithOptions(reader, FrameOptions{
		MaxFrameSize: DefaultMaxFrame,
		ReadTimeout:  50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ReadFrameWithOptions error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("payload mismatch: got %q want %q", got, want)
	}
	if reader.readDeadline.IsZero() {
		t.Fatal("expected read deadline to be set")
	}
}

func TestReadFrameTimesOutForSlowBody(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()

	done := make(chan error, 1)
	go func() {
		var header [FrameHeaderSize]byte
		binary.BigEndian.PutUint32(header[:], 8)
		_, err := writer.Write(header[:])
		done <- err
	}()

	_, err := ReadFrameWithOptions(reader, FrameOptions{
		MaxFrameSize: DefaultMaxFrame,
		ReadTimeout:  20 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}

	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout error, got %v", err)
	}

	if err := <-done; err != nil {
		t.Fatalf("header write error: %v", err)
	}
}

func TestWriteFrameHandlesPartialWrites(t *testing.T) {
	payload := []byte("partial write payload")
	writer := &chunkedWriter{chunkSize: 3}

	if err := WriteFrame(writer, payload, DefaultMaxFrame); err != nil {
		t.Fatalf("WriteFrame error: %v", err)
	}

	got, err := ReadFrame(bytes.NewReader(writer.buf.Bytes()), DefaultMaxFrame)
	if err != nil {
		t.Fatalf("ReadFrame after WriteFrame error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	err := WriteFrameWithOptions(&bytes.Buffer{}, []byte("toolong"), FrameOptions{MaxFrameSize: 3})
	if err == nil {
		t.Fatal("expected invalid length error")
	}
}

func TestWriteFrameAppliesWriteDeadline(t *testing.T) {
	payload := []byte("deadline payload")
	writer := &deadlineWriteBuffer{}

	if err := WriteFrameWithOptions(writer, payload, FrameOptions{
		MaxFrameSize: DefaultMaxFrame,
		WriteTimeout: 50 * time.Millisecond,
	}); err != nil {
		t.Fatalf("WriteFrameWithOptions error: %v", err)
	}
	if writer.writeDeadline.IsZero() {
		t.Fatal("expected write deadline to be set")
	}

	got, err := ReadFrame(bytes.NewReader(writer.buf.Bytes()), DefaultMaxFrame)
	if err != nil {
		t.Fatalf("ReadFrame after WriteFrameWithOptions error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
}
