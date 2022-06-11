package log

import (
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/tysonmote/gommap"
)

// インデックスエントリを構成するバイト数を定義する
const (
	// レコードのオフセット
	offWidth uint64 = 4
	// ストアファイル内の位置
	posWidth uint64 = 8
	// エントリの位置
	entWidth = offWidth + posWidth
)

type index struct {
	// 永続化されたファイル
	file *os.File
	// メモリにマップされたファイル
	mmap gommap.MMap
	// インデックスのサイズ
	size uint64
}

// 指定されたファイルのインデックスを作成する
func newIndex(f *os.File, c Config) (*index, error) {
	idx := &index{
		file: f,
	}
	fi, err := os.Stat(f.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	// 現在のサイズを取得する
	// これにより、インデックスエントリを追加するときにインデックスファイル内の
	// データ量を追跡することができる
	idx.size = uint64(fi.Size())
	// ファイルをメモリマッピングする前に最大インデックスサイズまで成長させる
	err = os.Truncate(f.Name(), int64(c.Segment.MaxIndexBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to truncate: %w", err)
	}
	idx.mmap, err = gommap.Map(idx.file.Fd(), gommap.PROT_READ|gommap.PROT_WRITE, gommap.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to get mmap: %w", err)
	}
	return idx, nil
}

func (i *index) Close() error {
	// memory mapped fileがそのデータを永続化ファイルに同期したことを確認する
	if err := i.mmap.Sync(gommap.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}
	// 永続化ファイルがその内容をストレージにFlushしたかどうか確認する
	if err := i.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}
	// ファイルを実際に存在するデータ量に切り詰める
	// こうしないと正確なファイルの最後のバイトがわからなくなり
	// サービスを正しく再起動することができなくなる
	// しかしグレースフルではないシャットダウンが発生した場合は
	// サービスの再起動時にサニティチェックなどを行って
	// 破損したデータを修復する必要がある(このコードでは対応していない)
	if err := i.file.Truncate(int64(i.size)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}
	// ファイルを閉じる
	return errors.WithMessage(i.file.Close(), "failed to close file")
}

// オフセットを受け取り、ストア内の関連するレコードの位置を返す
func (i *index) Read(in int64) (out uint32, pos uint64, err error) {
	if i.size == 0 {
		return 0, 0, io.EOF
	}
	if in == -1 {
		out = uint32((i.size / entWidth) - 1)
	} else {
		out = uint32(in)
	}
	pos = uint64(out) * entWidth
	if i.size < pos+entWidth {
		return 0, 0, io.EOF
	}
	// 相対オフセットを使うことでインデックスのサイズをuint32で表現でき小さくすることができる
	out = enc.Uint32(i.mmap[pos : pos+offWidth])
	// 絶対オフセットを使うと各エントリにさらに4バイト必要になる
	pos = enc.Uint64(i.mmap[pos+offWidth : pos+entWidth])
	return out, pos, nil
}

// 与えられたオフセットと位置をインデックスに追加する
func (i *index) Write(off uint32, pos uint64) error {
	// エントリを書き込むためのスペースがあるかチェックする
	if uint64(len(i.mmap)) < i.size+entWidth {
		return io.EOF
	}
	// スペースがあれば、オフセットと位置をエンコードして
	// メモリマップドファイルに書き込む
	enc.PutUint32(i.mmap[i.size:i.size+offWidth], off)
	enc.PutUint64(i.mmap[i.size+offWidth:i.size+entWidth], pos)
	// 次の書き込みが行われる位置を書き込む
	i.size += entWidth
	return nil
}

// インデックスのファイルパスを返す
func (i *index) Name() string {
	return i.file.Name()
}
