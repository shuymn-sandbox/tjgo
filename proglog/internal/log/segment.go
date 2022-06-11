package log

import (
	"fmt"
	"os"
	"path"

	api "github.com/shuymn-sandbox/tjgo/proglog/api/log/v1"
	"google.golang.org/protobuf/proto"
)

// セグメントはインデックスとストアをラップして両者にまたがる操作をする
type segment struct {
	store                  *store
	index                  *index
	baseOffset, nextOffset uint64
	config                 Config
}

const (
	storeFilePerm = 0o644
	indexFilePerm = 0o644
)

// アクティブなセグメントが最大サイズに達したときなど、新しいセグメントを追加する必要があるときに呼び出す
func newSegment(dir string, baseOffset uint64, c Config) (*segment, error) {
	s := &segment{
		baseOffset: baseOffset,
		config:     c,
	}
	// ストアファイルが無かったら作る
	storeFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".store")),
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		storeFilePerm,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open store file: %w", err)
	}
	s.store, err = newStore(storeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create new store: %w", err)
	}
	// インデックスファイルが無かったら作る
	indexFile, err := os.OpenFile(
		path.Join(dir, fmt.Sprintf("%d%s", baseOffset, ".index")),
		os.O_RDWR|os.O_CREATE,
		indexFilePerm,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %w", err)
	}
	s.index, err = newIndex(indexFile, c)
	if err != nil {
		return nil, fmt.Errorf("failed to create index file: %w", err)
	}
	// インデックスに一つでもエントリがあれば、次に書き込むレコードのオフセットは
	// セグメントの末尾のオフセットになる
	off, _, err := s.index.Read(-1)
	if err != nil {
		s.nextOffset = baseOffset
	} else {
		// セグメントの末尾のオフセットはベースオフセット+相対オフセット+1したもの
		s.nextOffset = baseOffset + uint64(off) + 1
	}
	return s, nil
}

// セグメントにレコードを書き込み、新しく追加されたレコードのオフセットを返す
func (s *segment) Append(record *api.Record) (offset uint64, err error) {
	cur := s.nextOffset
	record.Offset = cur
	p, err := proto.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal record: %w", err)
	}
	// データをストアに追加する
	_, pos, err := s.store.Append(p)
	if err != nil {
		return 0, fmt.Errorf("failed to append to store: %w", err)
	}
	// インデックスエントリを追加する
	err = s.index.Write(uint32(s.nextOffset-s.baseOffset), pos)
	if err != nil {
		return 0, fmt.Errorf("failed to write index: %w", err)
	}
	// 次の呼び出しのためにインクリメントする
	s.nextOffset += 1
	return cur, nil
}

// 与えられたオフセットのレコードを返す
func (s *segment) Read(off uint64) (*api.Record, error) {
	// 絶対インデックスを相対オフセットに変換し、関連するインデックスエントリを取得する
	_, pos, err := s.index.Read(int64(off - s.baseOffset))
	if err != nil {
		return nil, fmt.Errorf("failed to read index: %w", err)
	}
	// ストア内のそのレコードの位置に移動して必要なデータを読み込む
	p, err := s.store.Read(pos)
	if err != nil {
		return nil, fmt.Errorf("failed to read store: %w", err)
	}
	record := &api.Record{}
	if err = proto.Unmarshal(p, record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal record: %w", err)
	}
	return record, nil
}

// セグメントが最大サイズに達したかどうかを返す
func (s *segment) IsMaxed() bool {
	return s.store.size >= s.config.Segment.MaxStoreBytes || s.index.size >= s.config.Segment.MaxIndexBytes
}

// セグメントを閉じ、インデックスファイルとストアファイルを削除する
func (s *segment) Remove() error {
	if err := s.Close(); err != nil {
		return fmt.Errorf("failed to close segment: %w", err)
	}
	if err := os.Remove(s.index.Name()); err != nil {
		return fmt.Errorf("failed to remove index: %w", err)
	}
	if err := os.Remove(s.store.Name()); err != nil {
		return fmt.Errorf("failed to remove store: %w", err)
	}
	return nil
}

func (s *segment) Close() error {
	if err := s.index.Close(); err != nil {
		return fmt.Errorf("failed to close index: %w", err)
	}
	if err := s.store.Close(); err != nil {
		return fmt.Errorf("failed to close store: %w", err)
	}
	return nil
}
