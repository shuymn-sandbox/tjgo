package log

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"github.com/pkg/errors"
)

// レコードサイズとインデックスエントリを永続化するためのエンコーディングを定義する
var enc = binary.BigEndian

// レコードの長さを格納するために使うバイト数
const lenWidth = 8

// ファイルへのバイトの追加と読み込みを行うAPIをもつファイルのラッパー
type store struct {
	*os.File
	mu   sync.Mutex
	buf  *bufio.Writer
	size uint64
}

func newStore(f *os.File) (*store, error) {
	fi, err := os.Stat(f.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	// 現在のファイルサイズを取得する
	// 既存のデータがあるファイルからストアを作成する場合(再起動など)で発生する
	size := uint64(fi.Size())
	return &store{
		File: f,
		size: size,
		buf:  bufio.NewWriter(f),
	}, nil
}

// 与えられたバイトをストアに永続化する
func (s *store) Append(p []byte) (n uint64, pos uint64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pos = s.size
	// レコードの長さを書くことで、レコードを読むときに何バイト読めば良いのかわかるようにする
	if err = binary.Write(s.buf, enc, uint64(len(p))); err != nil {
		return 0, 0, fmt.Errorf("failed to write record length: %w", err)
	}
	// 直接ファイルに書き込むのではなく、Buffered Writerに書き込むことでシステムコールの回数を減らしてパフォーマンスを向上させる
	// 特に小さなレコードをたくさん書き込むときに有効
	w, err := s.buf.Write(p)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to write: %w", err)
	}
	w += lenWidth
	s.size += uint64(w)
	return uint64(w), pos, nil
}

// 指定された位置に格納されているレコードを返す
func (s *store) Read(pos uint64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// BufferがまだディスクにFlushしていないレコードを読もうとする場合に備えてまずFlushする
	if err := s.buf.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush: %w", err)
	}
	// レコード全体を読むために何バイト読まないといけないのかを調べる
	size := make([]byte, lenWidth)
	if _, err := s.File.ReadAt(size, int64(pos)); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	// レコードを読む
	b := make([]byte, enc.Uint64(size))
	if _, err := s.File.ReadAt(b, int64(pos+lenWidth)); err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return b, nil
}

// ストアのファイルの中のoffから始まるpにlen(p)バイトを読み込む
func (s *store) ReadAt(p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.buf.Flush(); err != nil {
		return 0, fmt.Errorf("failed to flush: %w", err)
	}
	n, err := s.File.ReadAt(p, off)
	return n, errors.WithMessage(err, "failed to read file")
}

// ファイルを閉じる前にバッファされたデータを永続化
func (s *store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.buf.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}
	return errors.WithMessage(s.File.Close(), "failed to close file")
}
