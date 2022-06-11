package log_test

import (
	"io"
	"io/ioutil"
	"os"
	"testing"

	api "github.com/shuymn-sandbox/tjgo/proglog/api/log/v1"
	"github.com/shuymn-sandbox/tjgo/proglog/internal/log"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestLog(t *testing.T) {
	testcases := map[string]func(t *testing.T, l *log.Log){
		"append and read a record succeeds": testAppendRead,
		"offset out of range error":         testOutOfRangeErr,
		"init with existing segments":       testInitExisting,
		"reader":                            testReader,
		"truncate":                          testTruncate,
	}

	for scenario, fn := range testcases {
		t.Run(scenario, func(t *testing.T) {
			dir, err := ioutil.TempDir("", "store-test")
			require.NoError(t, err)
			defer os.RemoveAll(dir)
			c := log.Config{}
			c.Segment.MaxStoreBytes = 32
			l, err := log.NewLog(dir, c)
			require.NoError(t, err)

			fn(t, l)
		})
	}
}

// ログへの追加とログからの読み込みが正常に行われるかテストする
func testAppendRead(t *testing.T, l *log.Log) {
	record := &api.Record{
		Value: []byte("hello world"),
	}
	// ログにレコードを追加すると、ログはそのレコードに関連付けられたオフセットを返す
	off, err := l.Append(record)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	// ログにそのオフセットにあるレコードを要求すると、追加したのと同じレコードが得られる
	read, err := l.Read(off)
	require.NoError(t, err)
	require.Equal(t, record.Value, read.Value)
}

// ログが格納しているオフセットの範囲外のオフセットを読み取ろうとするとエラーを返すかテストする
func testOutOfRangeErr(t *testing.T, l *log.Log) {
	read, err := l.Read(1)
	require.Nil(t, read)
	require.Error(t, err)
}

// ログを作成するときにログが以前のインスタンスによって保存されたデータから自分自身をブートストラップできるかテストする
func testInitExisting(t *testing.T, o *log.Log) {
	// ログに3つのレコードを追加する
	record := &api.Record{
		Value: []byte("hello world"),
	}
	for i := 0; i < 3; i++ {
		_, err := o.Append(record)
		require.NoError(t, err)
	}
	// ログを閉じる
	require.NoError(t, o.Close())

	off, err := o.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)
	off, err = o.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)

	// 古いログと同じディレクトリで構成された新しいログを作成する
	n, err := log.NewLog(o.Dir, o.Config)
	require.NoError(t, err)

	// 新しいログが元のログによって保存されたデータから自分自身をセットアップできたか確認する
	off, err = n.LowestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)
	off, err = n.HighestOffset()
	require.NoError(t, err)
	require.Equal(t, uint64(2), off)
}

// ログをスナップショットして復元できるように、ディスクに保存されている完全な生のログを読み取ることができるかテストする
func testReader(t *testing.T, l *log.Log) {
	record := &api.Record{
		Value: []byte("hello world"),
	}
	off, err := l.Append(record)
	require.NoError(t, err)
	require.Equal(t, uint64(0), off)

	reader := l.Reader()
	b, err := io.ReadAll(reader)
	require.NoError(t, err)

	read := &api.Record{}
	err = proto.Unmarshal(b[log.ExportLenWidth:], read)
	require.NoError(t, err)
	require.Equal(t, record.Value, read.Value)
}

// ログを切り詰めて必要の無いセグメントを削除できるかテストする
func testTruncate(t *testing.T, l *log.Log) {
	record := &api.Record{
		Value: []byte("hello world"),
	}
	for i := 0; i < 3; i++ {
		_, err := l.Append(record)
		require.NoError(t, err)
	}

	err := l.Truncate(1)
	require.NoError(t, err)

	_, err = l.Read(0)
	require.Error(t, err)
}
