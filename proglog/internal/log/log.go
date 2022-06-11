package log

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	api "github.com/shuymn-sandbox/tjgo/proglog/api/log/v1"
)

var baseDecimal = 10

type Log struct {
	mu sync.RWMutex

	Dir    string
	Config Config

	activeSegment *segment
	segments      []*segment
}

func NewLog(dir string, c Config) (*Log, error) {
	if c.Segment.MaxStoreBytes == 0 {
		c.Segment.MaxStoreBytes = 1024
	}
	if c.Segment.MaxIndexBytes == 0 {
		c.Segment.MaxIndexBytes = 1024
	}
	l := &Log{
		Dir:    dir,
		Config: c,
	}
	if err := l.setup(); err != nil {
		return nil, fmt.Errorf("failed to setup log: %w", err)
	}
	return l, nil
}

func (l *Log) setup() error {
	// ディスク上にすでに存在するセグメントに対して自分自身をセットアップする
	files, err := os.ReadDir(l.Dir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}
	var baseOffsets []uint64
	for _, file := range files {
		offStr := strings.TrimSuffix(file.Name(), path.Ext(file.Name()))
		off, _ := strconv.ParseUint(offStr, baseDecimal, 0)
		baseOffsets = append(baseOffsets, off)
	}
	sort.Slice(baseOffsets, func(i, j int) bool {
		return baseOffsets[i] < baseOffsets[j]
	})
	for i := 0; i < len(baseOffsets); i++ {
		if err = l.newSegment(baseOffsets[i]); err != nil {
			return fmt.Errorf("failed to create new segment with base offset: %w", err)
		}
		i++
	}
	// 既存のセグメントがない場合は最初のセグメントをブートストラップする
	if l.segments == nil {
		err = l.newSegment(l.Config.Segment.InitialOffset)
		if err != nil {
			return fmt.Errorf("failed to create new segment with initial offset: %w", err)
		}
	}
	return nil
}

// ログにレコードを追加する
func (l *Log) Append(record *api.Record) (uint64, error) {
	// RWMutexを使う
	// ロックを保持している書き込みがない場合は読み取りへのアクセスを許可する
	// よりパフォーマンスを求めるのであれば、セグメントごとにロックを作ることもできるが、ここではしていない
	l.mu.Lock()
	defer l.mu.Unlock()
	// レコードはアクティブなセグメントに追加する
	off, err := l.activeSegment.Append(record)
	if err != nil {
		return 0, fmt.Errorf("failed to append to active segment: %w", err)
	}
	// 最大サイズになったら次のアクティブなセグメントを作る
	if l.activeSegment.IsMaxed() {
		err = l.newSegment(off + 1)
	}
	return off, err
}

// 与えられたオフセットに格納されているレコードを読み取る
func (l *Log) Read(off uint64) (*api.Record, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	// 与えられたオフセットのセグメントを探す
	var s *segment
	for _, segment := range l.segments {
		// セグメントのベースオフセットはセグメント内の最小のオフセットなので
		// ベースオフセットが指定されたオフセット以下である最初のオフセットが見つかるまで調べる
		if segment.baseOffset <= off && off < segment.nextOffset {
			s = segment
			break
		}
	}
	if s == nil || s.nextOffset <= off {
		return nil, fmt.Errorf("offset: %d: %w", off, ErrOffsetOutOfRange)
	}
	// セグメントのインデックスからインデックスエントリを取得し、ストアファイルからデータを読み出す
	return s.Read(off)
}

var ErrOffsetOutOfRange = errors.New("offset out of range")

// セグメントをすべて閉じる
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, segment := range l.segments {
		if err := segment.Close(); err != nil {
			return fmt.Errorf("failed to close segment: %w", err)
		}
	}
	return nil
}

// ログを閉じてからそのデータを削除する
func (l *Log) Remove() error {
	if err := l.Close(); err != nil {
		return fmt.Errorf("failed to close log: %w", err)
	}
	if err := os.RemoveAll(l.Dir); err != nil {
		return fmt.Errorf("failed to remove log directory: %w", err)
	}
	return nil
}

// ログを削除してからそのログを置き換えるための新しいログを作る
func (l *Log) Reset() error {
	if err := l.Remove(); err != nil {
		return fmt.Errorf("failed to remove log: %w", err)
	}
	return l.setup()
}

func (l *Log) LowestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.segments[0].baseOffset, nil
}

func (l *Log) HighestOffset() (uint64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	off := l.segments[len(l.segments)-1].nextOffset
	if off == 0 {
		return 0, nil
	}
	return off - 1, nil
}

// 一番大きなオフセットがlowestよりも小さいセグメントをすべて削除する
// ディスクの容量は無限ではないので、定期的にTruncateして古いセグメントを削除する
func (l *Log) Truncate(lowest uint64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var segments []*segment
	for _, s := range l.segments {
		if s.nextOffset <= lowest+1 {
			if err := s.Remove(); err != nil {
				return fmt.Errorf("failed to remove segment: %w", err)
			}
			continue
		}
		segments = append(segments, s)
	}
	l.segments = segments
	return nil
}

// ログ全体を読み込むためのio.Readerを返す
// スナップショットやログの復元をサポートする必要があるときに必要になる
func (l *Log) Reader() io.Reader {
	l.mu.RLock()
	defer l.mu.RUnlock()
	readers := make([]io.Reader, len(l.segments))
	for i, segment := range l.segments {
		readers[i] = &originReader{segment.store, 0}
	}
	// セグメントのストアをoriginReaderでラップしてMultiReaderとする
	// ストアファイルの全体を読み込むことを保証する
	return io.MultiReader(readers...)
}

type originReader struct {
	*store
	off int64
}

// io.Readerインターフェースを満たす
func (o *originReader) Read(p []byte) (int, error) {
	n, err := o.ReadAt(p, o.off)
	o.off += int64(n)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	return n, err
}

func (l *Log) newSegment(off uint64) error {
	// 新しいセグメントを作る
	s, err := newSegment(l.Dir, off, l.Config)
	if err != nil {
		return fmt.Errorf("failed to initialize segment: %w", err)
	}
	// 作ったセグメントをログのセグメントスライスに追加する
	l.segments = append(l.segments, s)
	// それをアクティブなセグメントにする
	l.activeSegment = s
	return nil
}
