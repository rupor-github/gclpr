package util

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
)

func TestWriteReadFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"small", []byte("hello")},
		{"one_byte", []byte{0x42}},
		{"binary_data", []byte{0, 1, 2, 255, 254, 253}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tc.data); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}

			// Verify wire format: 4-byte big-endian length + payload
			wire := buf.Bytes()
			if len(wire) != 4+len(tc.data) {
				t.Fatalf("wire length = %d, want %d", len(wire), 4+len(tc.data))
			}
			gotLen := binary.BigEndian.Uint32(wire[:4])
			if gotLen != uint32(len(tc.data)) {
				t.Fatalf("length prefix = %d, want %d", gotLen, len(tc.data))
			}

			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Fatalf("ReadFrame returned %x, want %x", got, tc.data)
			}
		})
	}
}

func TestLargePayload(t *testing.T) {
	// 256 KiB -- larger than a typical TCP segment
	data := make([]byte, 256*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, data); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("large payload mismatch")
	}
}

func TestMultipleFrames(t *testing.T) {
	messages := [][]byte{
		[]byte("first"),
		[]byte("second message"),
		[]byte("third"),
		{}, // empty frame
		[]byte("fifth"),
	}

	var buf bytes.Buffer
	for _, msg := range messages {
		if err := WriteFrame(&buf, msg); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}

	for i, want := range messages {
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame[%d]: %v", i, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("frame %d: got %q, want %q", i, got, want)
		}
	}

	// No more frames
	_, err := ReadFrame(&buf)
	if err != io.EOF {
		t.Fatalf("expected EOF after all frames, got %v", err)
	}
}

func TestReadFrameExceedsMaxSize(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], MaxFrameSize+1)
	buf.Write(hdr[:])
	// Don't need to write actual payload -- ReadFrame should reject based on length

	_, err := ReadFrame(&buf)
	if err == nil {
		t.Fatal("expected error for oversized frame, got nil")
	}
}

func TestReadFrameTruncatedHeader(t *testing.T) {
	// Only 2 bytes instead of 4
	buf := bytes.NewReader([]byte{0, 1})
	_, err := ReadFrame(buf)
	if err == nil {
		t.Fatal("expected error for truncated header, got nil")
	}
}

func TestReadFrameTruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 100)
	buf.Write(hdr[:])
	buf.Write([]byte("short")) // only 5 bytes instead of 100

	_, err := ReadFrame(&buf)
	if err == nil {
		t.Fatal("expected error for truncated payload, got nil")
	}
}

func TestFramingOverTCP(t *testing.T) {
	// Test that framing works correctly over a real TCP connection
	// where the OS may split or coalesce writes.
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	messages := [][]byte{
		[]byte("hello"),
		make([]byte, 128*1024), // 128 KiB
		[]byte("after large"),
		{}, // empty
		[]byte("final"),
	}
	// Fill the large message with random data
	if _, err := rand.Read(messages[1]); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var serverErr error

	// Server side: read all frames
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			serverErr = err
			return
		}
		defer conn.Close()

		for i, want := range messages {
			got, err := ReadFrame(conn)
			if err != nil {
				serverErr = err
				return
			}
			if !bytes.Equal(got, want) {
				t.Errorf("frame %d: payload mismatch", i)
				return
			}
		}

		// Echo back a response
		if err := WriteFrame(conn, []byte("all received")); err != nil {
			serverErr = err
		}
	}()

	// Client side: write all frames
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for _, msg := range messages {
		if err := WriteFrame(conn, msg); err != nil {
			t.Fatalf("client WriteFrame: %v", err)
		}
	}

	// Read the echo response
	resp, err := ReadFrame(conn)
	if err != nil {
		t.Fatalf("client ReadFrame: %v", err)
	}
	if string(resp) != "all received" {
		t.Fatalf("response = %q, want %q", resp, "all received")
	}

	wg.Wait()
	if serverErr != nil {
		t.Fatalf("server error: %v", serverErr)
	}
}
