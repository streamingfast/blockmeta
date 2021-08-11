package blockmeta

import (
	"context"
	"errors"
	"time"

	"github.com/streamingfast/bstream"
)

var ErrDBOutOfSync = errors.New("database is out of sync")
var ErrNotFound = errors.New("not found")
var ErrNotImplemented = errors.New("not implemented")

type BlockmetaDB interface {
	BlockIDAt(ctx context.Context, start time.Time) (id string, err error)
	BlockIDAfter(ctx context.Context, start time.Time, inclusive bool) (id string, foundtime time.Time, err error)
	BlockIDBefore(ctx context.Context, start time.Time, inclusive bool) (id string, foundtime time.Time, err error)

	GetLastWrittenBlockID(ctx context.Context) (blockID string, err error)
	GetIrreversibleIDAtBlockNum(ctx context.Context, num uint64) (ref bstream.BlockRef, err error)
	GetIrreversibleIDAtBlockID(ctx context.Context, id string) (ref bstream.BlockRef, err error)

	// GetForkPreviousBlocks will give you the blockrefs of all blocks in this fork.
	// It stops going up when it meets an irreversible block in kvdb, or if a block is unlinkable (previousID not found in db).
	GetForkPreviousBlocks(ctx context.Context, forkTop bstream.BlockRef) ([]bstream.BlockRef, error)
}
