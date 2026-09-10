package crypto

import (
	"errors"
	"fmt"
	"io"
)

type AESGCMDecryptStream struct {
	crypto     *AESGCMHelper
	source     io.ReadCloser
	buf        []byte
	unread     []byte
	chunkIndex uint32
	done       bool
}

var (
	_ io.WriterTo   = (*AESGCMDecryptStream)(nil)
	_ io.ReadCloser = (*AESGCMDecryptStream)(nil)
)

func (s *AESGCMDecryptStream) initStream() error {
	if s.buf != nil {
		return nil
	}
	header := make([]byte, 3)
	_, err := io.ReadFull(s.source, header)
	if err != nil {
		return err
	} else if header[0] != 0 {
		return fmt.Errorf("invalid first-byte header signature (got=%o, expected=%o)", header[0], 0)
	}
	rawChunkSize := 1 << header[1]
	if rawChunkSize > maxRawChunkSize {
		return fmt.Errorf("chunk size too large (%d > %d)", rawChunkSize, maxRawChunkSize)
	}
	s.buf = make([]byte, rawChunkSize+1)
	s.buf[len(s.buf)-1] = header[2]
	return nil
}

func (s *AESGCMDecryptStream) readChunk() ([]byte, error) {
	err := s.initStream()
	if err != nil {
		return nil, err
	}
	s.buf[0] = s.buf[len(s.buf)-1]
	n, err := io.ReadFull(s.source, s.buf[1:])
	isLastChunk := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	if err != nil && (n == 0 || !isLastChunk) {
		return nil, fmt.Errorf("failed to read chunk #%d: %w", s.chunkIndex+1, err)
	}
	chunk := s.buf[:len(s.buf)-1]
	if isLastChunk {
		chunk = s.buf[:n+1]
		s.done = true
	}
	aad := s.crypto.calculateAAD(s.chunkIndex, isLastChunk)
	decryptedChunk, err := s.crypto.decryptChunk(chunk, aad)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt chunk #%d: %w", s.chunkIndex+1, err)
	}
	s.chunkIndex++
	return decryptedChunk, nil
}

func (s *AESGCMDecryptStream) Read(p []byte) (n int, err error) {
	if len(s.unread) == 0 {
		if s.done {
			return 0, io.EOF
		}
		s.unread, err = s.readChunk()
		if err != nil {
			return
		}
	}
	n = copy(p, s.unread)
	s.unread = s.unread[n:]
	return
}

func (s *AESGCMDecryptStream) WriteTo(w io.Writer) (n int64, err error) {
	if len(s.unread) > 0 {
		m, err := w.Write(s.unread)
		if err != nil {
			return n, fmt.Errorf("failed to write existing unread data: %w", err)
		}
		s.unread = nil
		n += int64(m)
	}
	for !s.done {
		chunk, err := s.readChunk()
		if err != nil {
			return n, err
		}
		m, err := w.Write(chunk)
		if err == nil && m < len(chunk) {
			err = io.ErrShortWrite
		}
		if err != nil {
			return n, fmt.Errorf("failed to write chunk #%d: %w", s.chunkIndex, err)
		}
		n += int64(m)
	}
	return
}

func (s *AESGCMDecryptStream) Close() error {
	return s.source.Close()
}
