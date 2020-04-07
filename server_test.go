// Copyright 2019 dfuse Platform Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package blockmeta

import (
	"context"
	"fmt"
	"testing"
	"time"

	pbblockmeta "github.com/dfuse-io/pbgo/dfuse/blockmeta/v1"
	pbbstream "github.com/dfuse-io/pbgo/dfuse/bstream/v1"
	"github.com/dfuse-io/bstream"
	"github.com/dfuse-io/bstream/forkable"
	"github.com/eoscanada/eos-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var blockDateLayout = "2006-01-02T15:04:05.000"
var refTime = time.Date(2019, 01, 01, 20, 00, 00, 500000000, time.UTC)

func TestServer_InLongestChain(t *testing.T) {

	s, db, src := setupServer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	bt := func(seconds int) time.Time {
		return refTime.Add(time.Duration(seconds) * time.Second)
	}

	require.NoError(t, src.Push(block("00000001a", "00000000a", 0, bt(1)), nil)) // will be ignored
	require.NoError(t, src.Push(block("00000002a", "00000001a", 1, bt(2)), nil)) // set lib to 1
	require.NoError(t, src.Push(block("00000002b", "00000001a", 1, bt(3)), nil)) // set lib to 1
	require.NoError(t, src.Push(block("00000003b", "00000002b", 1, bt(4)), nil)) // set lib to 1

	// from forkDB
	r, err := s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "00000002a"})
	assert.NoError(t, err)
	assert.False(t, r.InLongestChain)
	assert.False(t, r.Irreversible)

	r, err = s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "00000002b"})
	assert.NoError(t, err)
	assert.True(t, r.InLongestChain)
	assert.False(t, r.Irreversible)

	// from eosDB
	require.NoError(t, src.Push(block("00000003a", "00000002a", 2, bt(5)), nil))
	require.NoError(t, src.Push(block("00000004a", "00000003a", 3, bt(6)), nil))

	db.nextID = "00000002a"
	r, err = s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "00000002a"})
	assert.NoError(t, err)
	assert.True(t, r.InLongestChain)
	assert.True(t, r.Irreversible)

	r, err = s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "00000002b"})
	assert.NoError(t, err)
	assert.False(t, r.InLongestChain)
	assert.False(t, r.Irreversible)

	db.nextError = fmt.Errorf("other error")
	r, err = s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "wut"})
	assert.Error(t, err)

	db.nextID = "00000002a"
	db.nextError = nil
	r, err = s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "00000003b"})
	assert.Equal(t, ErrDBOutOfSync, err)

	require.NoError(t, src.Push(block("00000005a", "00000004a", 4, bt(7)), nil))
	require.NoError(t, src.Push(block("00000006a", "00000005a", 5, bt(8)), nil))
	require.NoError(t, src.Push(block("00000007a", "00000006a", 6, bt(9)), nil))
	require.NoError(t, src.Push(block("00000008a", "00000007a", 7, bt(10)), nil))

	db.nextError = fmt.Errorf("unwanted error")
	s.purgeOldIrrBlocksFromMap()
	r, err = s.InLongestChain(ctx, &pbblockmeta.InLongestChainRequest{BlockID: "00000005a"})
	assert.NoError(t, err)
	assert.True(t, r.InLongestChain)
	assert.True(t, r.Irreversible)

}

func setupServer() (*server, *fakeDB, *bstream.TestSource) {
	srcFactory := bstream.NewTestSourceFactory()
	db := &fakeDB{}
	s := NewServer("", "", nil, db, nil, nil, pbbstream.Protocol_EOS)
	s.initialStartBlockID = "00000001a"
	frkable := forkable.New(s, forkable.WithName("blockmeta"))
	srcFactory.NewSourceFromRef(bstream.NewBlockRefFromID("00000001a"), frkable)
	src := <-srcFactory.Created
	s.src = src
	go s.launch()
	s.ready.Store(true)
	return s, db, src
}

func TestServer_TimeToID_At_WithFork(t *testing.T) {

	s, _, src := setupServer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	time := func(seconds int) time.Time {
		return refTime.Add(time.Duration(seconds) * time.Second)
	}

	require.NoError(t, src.Push(block("00000002a", "00000001a", 1, time(1)), nil))
	require.NoError(t, src.Push(block("00000003a", "00000002a", 2, time(2)), nil))
	require.NoError(t, src.Push(block("00000003b", "00000002a", 2, time(3)), nil))
	require.NoError(t, src.Push(block("00000004b", "00000003b", 2, time(4)), nil))

	r, err := s.At(ctx, &pbblockmeta.TimeRequest{Time: TimestampProto(time(3))})
	assert.NoError(t, err)
	assert.Equal(t, "00000003b", r.Id)
	assert.False(t, r.Irreversible)

	require.NoError(t, src.Push(block("00000004a", "00000003a", 3, time(5)), nil))
	require.NoError(t, src.Push(block("00000005a", "00000004a", 3, time(6)), nil))

	assert.EqualValues(t, s.lib.Time(), (time(2)), "lib set correctly")

	_, err = s.At(ctx, &pbblockmeta.TimeRequest{Time: TimestampProto(time(3))})
	assert.Error(t, err, "block id was not found") //block is bye bye because and undo ...

}

func TestServer_TimeToID_At(t *testing.T) {

	s, db, src := setupServer()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	time := func(seconds int) time.Time {
		return refTime.Add(time.Duration(seconds) * time.Second)
	}

	require.NoError(t, src.Push(block("00000001a", "00000000a", 0, time(1)), nil)) // will be ignored
	require.NoError(t, src.Push(block("00000002a", "00000001a", 1, time(2)), nil)) // set lib to 1
	require.NoError(t, src.Push(block("00000003a", "00000002a", 2, time(5)), nil))
	require.NoError(t, src.Push(block("00000004a", "00000003a", 2, time(6)), nil))

	assert.True(t, s.lib.Time().Equal(time(2)), "lib set correctly")

	r, err := s.At(ctx, &pbblockmeta.TimeRequest{Time: TimestampProto(time(2))})
	assert.NoError(t, err)
	assert.Equal(t, "00000002a", r.Id)
	assert.True(t, r.Irreversible)

	r, err = s.At(ctx, &pbblockmeta.TimeRequest{Time: TimestampProto(time(5))})
	assert.NoError(t, err)
	assert.Equal(t, "00000003a", r.Id)
	assert.False(t, r.Irreversible)

	r, err = s.At(ctx, &pbblockmeta.TimeRequest{Time: TimestampProto(time(10))})
	assert.Error(t, err, "block id was not found")

	db.nextID = "00000000a"
	r, err = s.At(ctx, &pbblockmeta.TimeRequest{Time: TimestampProto(baseTestTime)})
	require.NoError(t, err)
	require.Equal(t, db.nextID, r.Id, "falling back to database")
	require.True(t, r.Irreversible)

}

func TestServer_TimeToID_After(t *testing.T) {

	s, db, src := setupServer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, src.Push(block("00000001a", "00000000a", 0, someTime(1)), nil)) // will be ignored
	require.NoError(t, src.Push(block("00000002a", "00000001a", 1, someTime(2)), nil)) // trigger forkdb move lib
	require.NoError(t, src.Push(block("00000003a", "00000002a", 2, someTime(3)), nil))
	require.NoError(t, src.Push(block("00000004a", "00000003a", 3, someTime(5)), nil))

	require.True(t, s.lib.Time().Equal(someTime(3)), "lib set correctly")

	r, err := s.After(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(someTime(2))})
	require.NoError(t, err)
	require.Equal(t, "00000003a", r.Id)

	r, err = s.After(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(someTime(3))})
	require.NoError(t, err)
	require.False(t, r.Irreversible)
	require.Equal(t, "00000004a", r.Id)

	r, err = s.After(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(someTime(6))})
	require.Error(t, err, "block id was not found")

	db.nextID = "00000000a"
	r, err = s.After(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(baseTestTime)})
	require.NoError(t, err)
	require.Equal(t, db.nextID, r.Id, "falling back to database")
	require.True(t, r.Irreversible)

}

func Test_TimeToID_Before(t *testing.T) {

	s, db, src := setupServer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	require.NoError(t, src.Push(block("00000001a", "00000000a", 0, someTime(1)), nil)) // will be ignored
	require.NoError(t, src.Push(block("00000002a", "00000001a", 1, someTime(2)), nil)) // trigger forkdb move lib
	require.NoError(t, src.Push(block("00000003a", "00000002a", 2, someTime(5)), nil))
	require.NoError(t, src.Push(block("00000004a", "00000003a", 3, someTime(6)), nil))
	require.NoError(t, src.Push(block("00000005a", "00000004a", 3, someTime(7)), nil))

	require.True(t, s.lib.Time().Equal(someTime(5)), "lib set correctly")

	r, err := s.Before(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(someTime(3))})
	require.NoError(t, err)
	require.Equal(t, "00000002a", r.Id)
	require.True(t, r.Irreversible)

	r, err = s.Before(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(someTime(7))})
	require.NoError(t, err)
	require.Equal(t, "00000004a", r.Id)
	require.False(t, r.Irreversible)

	db.nextID = "00000000a"
	r, err = s.Before(ctx, &pbblockmeta.RelativeTimeRequest{Time: TimestampProto(baseTestTime)})
	require.NoError(t, err)
	require.Equal(t, db.nextID, r.Id, "falling back to database")
	require.True(t, r.Irreversible)

}

var baseTestTime = time.Date(2019, 01, 01, 20, 00, 00, 500000000, time.UTC)

func someTime(seconds int) time.Time {
	return baseTestTime.Add(time.Duration(seconds) * time.Second)
}

func TestServer_purgeOldIrreversibleBlocks(t *testing.T) {

	s := server{
		blockTimes: make(map[string]time.Time),
	}

	s.blockTimes["00000001a"] = refTime.Add(1 * time.Second)
	s.blockTimes["00000002a"] = refTime.Add(2 * time.Second)
	s.blockTimes["00000003a"] = refTime.Add(3 * time.Second)
	s.blockTimes["00000004a"] = refTime.Add(4 * time.Second)
	s.blockTimes["00000005a"] = refTime.Add(5 * time.Second)
	s.keepDurationAfterLib = 1 * time.Second

	s.lib = block("00000002a", "00000001a", 1, refTime.Add(2*time.Second))
	s.purgeOldIrrBlocksFromMap()
	require.EqualValues(t, s.blockTimes, map[string]time.Time{
		"00000001a": refTime.Add(1 * time.Second),
		"00000002a": refTime.Add(2 * time.Second),
		"00000003a": refTime.Add(3 * time.Second),
		"00000004a": refTime.Add(4 * time.Second),
		"00000005a": refTime.Add(5 * time.Second),
	}, "nothing to purge below 1second")

	s.lib = block("00000005a", "00000004a", 4, refTime.Add(5*time.Second))
	s.purgeOldIrrBlocksFromMap()
	require.EqualValues(t, s.blockTimes, map[string]time.Time{
		"00000004a": refTime.Add(4 * time.Second),
		"00000005a": refTime.Add(5 * time.Second),
	}, "must purge everything lower than {5-1}")

}

func Test_lowestTime(t *testing.T) {
	s := server{
		blockTimes: make(map[string]time.Time),
	}

	s.blockTimes["00000001a"] = someTime(1)
	s.blockTimes["00000002a"] = someTime(2)
	s.blockTimes["00000003a"] = someTime(3)
	s.blockTimes["00000004a"] = someTime(4)
	s.blockTimes["00000005a"] = someTime(5)

	require.Equal(t, someTime(1), s.lowestTime())

}

type fakeDB struct {
	nextID    string
	nextTime  time.Time
	nextError error
}

func (_ fakeDB) GetForkPreviousBlocks(ctx context.Context, forkTop bstream.BlockRef) ([]bstream.BlockRef, error) {
	return nil, ErrNotImplemented
}
func (db fakeDB) BlockIDAt(context.Context, time.Time) (string, error) {
	return db.nextID, db.nextError
}

func (db fakeDB) BlockIDAfter(ctx context.Context, start time.Time, inclusive bool) (id string, foundtime time.Time, err error) {
	return db.nextID, db.nextTime, db.nextError
}

func (db fakeDB) BlockIDBefore(ctx context.Context, start time.Time, inclusive bool) (id string, foundtime time.Time, err error) {
	return db.nextID, db.nextTime, db.nextError
}

func (db fakeDB) GetLastWrittenBlockID(ctx context.Context) (string, error) {
	panic("not implemented")
}

func (db fakeDB) GetIrreversibleIDAtBlockNum(ctx context.Context, blockNum uint64) (bstream.BlockRef, error) {
	return bstream.NewBlockRef(db.nextID, uint64(eos.BlockNum(db.nextID))), db.nextError
}

func (db fakeDB) GetIrreversibleIDAtBlockID(ctx context.Context, id string) (bstream.BlockRef, error) {
	return bstream.NewBlockRef(db.nextID, uint64(eos.BlockNum(db.nextID))), db.nextError
}

func block(id, prev string, libNum uint64, timestamp time.Time) *bstream.Block {
	block := &bstream.Block{
		Id:         id,
		Number:     uint64(eos.BlockNum(id)),
		PreviousId: prev,
		LibNum:     libNum,
		Timestamp:  timestamp,
	}

	return block
}
