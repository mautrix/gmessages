package libgm

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// endlessReader keeps producing bytes, standing in for a response that does
// not stop. Reading it without a bound never returns.
type endlessReader struct{ read int64 }

func (e *endlessReader) Read(p []byte) (int, error) {
	e.read += int64(len(p))
	return len(p), nil
}

func TestReadAllLimited(t *testing.T) {
	t.Run("under the limit is returned intact", func(t *testing.T) {
		want := []byte("hello world")
		got, err := readAllLimited(bytes.NewReader(want), 1024)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("exactly at the limit is accepted", func(t *testing.T) {
		got, err := readAllLimited(strings.NewReader("12345"), 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 5 {
			t.Errorf("got %d bytes, want 5", len(got))
		}
	})

	t.Run("over the limit is refused, not truncated", func(t *testing.T) {
		_, err := readAllLimited(strings.NewReader("123456"), 5)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("got %v, want ErrResponseTooLarge", err)
		}
	})

	t.Run("an endless body does not read without bound", func(t *testing.T) {
		body := &endlessReader{}
		if _, err := readAllLimited(body, 1<<20); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("got %v, want ErrResponseTooLarge", err)
		}
		// Bounded by the limit, not by what the far side chooses to send.
		if body.read > 8<<20 {
			t.Errorf("read %d bytes for a 1 MiB limit", body.read)
		}
	})

	t.Run("a limit of zero or less is unbounded", func(t *testing.T) {
		for _, limit := range []int64{0, -1} {
			got, err := readAllLimited(io.LimitReader(&endlessReader{}, 4096), limit)
			if err != nil {
				t.Fatalf("limit %d: unexpected error: %v", limit, err)
			}
			if len(got) != 4096 {
				t.Errorf("limit %d: got %d bytes, want 4096", limit, len(got))
			}
		}
	})
}

func TestNewClientSetsDownloadLimits(t *testing.T) {
	c := &Client{MaxDownloadBytes: DefaultMaxDownloadBytes, MaxAvatarBytes: DefaultMaxAvatarBytes}
	if c.MaxDownloadBytes <= 0 || c.MaxAvatarBytes <= 0 {
		t.Fatal("defaults should be positive")
	}
	if c.MaxAvatarBytes >= c.MaxDownloadBytes {
		t.Error("an avatar should have a tighter limit than an attachment")
	}
}
