package log_test

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/shuymn-sandbox/tjgo/proglog/internal/log"
	"github.com/stretchr/testify/require"
)

var (
	write = []byte("hello world")
	width = uint64(len(write)) + log.ExportLenWidth
)

func TestStoreAppendRead(t *testing.T) {
	// 一時ファイルを作成
	f, err := ioutil.TempFile("", "store_append_read_test")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	// 一時ファイルを含むストアを作成
	s, err := log.ExportNewStore(f)
	require.NoError(t, err)

	// ストアへの追加と読み込みをテスト
	testAppend(t, s)
	testRead(t, s)
	testReadAt(t, s)

	// 再びストアを作成
	s, err = log.ExportNewStore(f)
	require.NoError(t, err)
	// もう一度読み込みをテストする
	// サービスの再起動後に状態が回復できることを確認する
	testRead(t, s)
}

func testAppend(t *testing.T, s *log.ExportStore) {
	t.Helper()
	for i := uint64(1); i < 4; i++ {
		n, pos, err := s.Append(write)
		require.NoError(t, err)
		require.Equal(t, pos+n, width*i)
	}
}

func testRead(t *testing.T, s *log.ExportStore) {
	t.Helper()
	var pos uint64
	for i := uint64(1); i < 4; i++ {
		read, err := s.Read(pos)
		require.NoError(t, err)
		require.Equal(t, write, read)
		pos += width
	}
}

func testReadAt(t *testing.T, s *log.ExportStore) {
	t.Helper()
	for i, off := uint64(1), int64(0); i < 4; i++ {
		b := make([]byte, log.ExportLenWidth)
		n, err := s.ReadAt(b, off)
		require.NoError(t, err)
		require.Equal(t, log.ExportLenWidth, n)
		off += int64(n)

		size := log.ExportEnc.Uint64(b)
		b = make([]byte, size)
		n, err = s.ReadAt(b, off)
		require.NoError(t, err)
		require.Equal(t, write, b)
		require.Equal(t, int(size), n)
		off += int64(n)
	}
}

func TestStoreClose(t *testing.T) {
	f, err := ioutil.TempFile("", "store_close_test")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	s, err := log.ExportNewStore(f)
	require.NoError(t, err)
	_, _, err = s.Append(write)
	require.NoError(t, err)

	f, beforeSize, err := openFile(f.Name())
	require.NoError(t, err)

	err = s.Close()
	require.NoError(t, err)

	_, afterSize, err := openFile(f.Name())
	require.NoError(t, err)
	require.True(t, afterSize > beforeSize)
}

func openFile(name string) (file *os.File, size int64, err error) {
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	return f, fi.Size(), nil
}
