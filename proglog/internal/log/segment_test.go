package log_test

import (
	"errors"
	"io"
	"io/ioutil"
	"os"
	"testing"

	api "github.com/shuymn-sandbox/tjgo/proglog/api/log/v1"
	"github.com/shuymn-sandbox/tjgo/proglog/internal/log"
	"github.com/stretchr/testify/require"
)

func TestSegment(t *testing.T) {
	dir, _ := ioutil.TempDir("", "segment-test")
	defer os.RemoveAll(dir)

	want := &api.Record{Value: []byte("hello world")}

	c := log.Config{}
	c.Segment.MaxStoreBytes = 1024
	c.Segment.MaxIndexBytes = log.ExportEntWidth * 3

	s, err := log.ExportNewSegment(dir, 16, c)
	require.NoError(t, err)
	require.Equal(t, uint64(16), s.ExportNextOffset(), s.ExportNextOffset())
	require.False(t, s.IsMaxed())

	// セグメントにレコードを追加する
	var (
		off uint64
		got *api.Record
	)
	for i := uint64(0); i < 3; i++ {
		off, err = s.Append(want)
		require.NoError(t, err)
		require.Equal(t, 16+i, off)

		got, err = s.Read(off)
		require.NoError(t, err)
		require.Equal(t, want.Value, got.Value)
	}

	_, err = s.Append(want)
	require.True(t, errors.Is(err, io.EOF))

	// インデックスが最大に達している状態
	require.True(t, s.IsMaxed())

	c.Segment.MaxStoreBytes = uint64(len(want.Value) * 3)
	c.Segment.MaxIndexBytes = 1024

	// 永続化されたインデックスとログファイルからセグメントの状態をロードできるか確認する
	s, err = log.ExportNewSegment(dir, 16, c)
	require.NoError(t, err)

	// ストアが最大に達している状態
	require.True(t, s.IsMaxed())

	err = s.Remove()
	require.NoError(t, err)
	s, err = log.ExportNewSegment(dir, 16, c)
	require.NoError(t, err)
	require.False(t, s.IsMaxed())
}
