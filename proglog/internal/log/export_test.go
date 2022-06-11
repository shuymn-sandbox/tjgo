package log

const (
	ExportLenWidth = lenWidth
	ExportEntWidth = entWidth
)

var (
	ExportEnc        = enc
	ExportNewStore   = newStore
	ExportNewIndex   = newIndex
	ExportNewSegment = newSegment
)

type ExportStore = store

func (s *segment) ExportNextOffset() uint64 {
	return s.nextOffset
}
