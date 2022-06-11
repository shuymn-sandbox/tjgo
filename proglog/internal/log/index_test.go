package log_test

import (
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/shuymn-sandbox/tjgo/proglog/internal/log"
	"github.com/stretchr/testify/require"
)

func TestIndex(t *testing.T) {
	f, err := ioutil.TempFile(os.TempDir(), "index_test")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	c := log.Config{}
	c.Segment.MaxIndexBytes = 1024
	idx, err := log.ExportNewIndex(f, c)
	require.NoError(t, err)
	_, _, err = idx.Read(-1)
	require.Error(t, err)
	require.Equal(t, f.Name(), idx.Name())

	entries := []struct {
		Off uint32
		Pos uint64
	}{
		{Off: 0, Pos: 0},
		{Off: 1, Pos: 10},
	}

	var pos uint64
	for _, want := range entries {
		// エントリを書き込む
		err = idx.Write(want.Off, want.Pos)
		require.NoError(t, err)

		_, pos, err = idx.Read(int64(want.Off))
		require.NoError(t, err)
		require.Equal(t, want.Pos, pos)
	}

	// 既存のエントリを超えて読み込もうとするとエラーになる
	_, _, err = idx.Read(int64(len(entries)))
	require.Equal(t, io.EOF, err)
	_ = idx.Close()

	// 既存のファイルから状態を構築する
	f, _ = os.OpenFile(f.Name(), os.O_RDWR, 0o600)
	idx, err = log.ExportNewIndex(f, c)
	require.NoError(t, err)
	off, pos, err := idx.Read(-1)
	require.NoError(t, err)
	require.Equal(t, uint32(1), off)
	require.Equal(t, entries[1].Pos, pos)
}
