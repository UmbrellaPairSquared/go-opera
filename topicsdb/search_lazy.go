package topicsdb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func (tt *Index) fetchLazy(topics [][]common.Hash, blockStart []byte, onLog func(*types.Log) bool) (err error) {
	_, err = tt.walk(nil, blockStart, topics, 0, onLog)
	return
}

// walk for topics recursive.
func (tt *Index) walk(
	rec *logrec, blockStart []byte, topics [][]common.Hash, pos uint8, onLog func(*types.Log) bool,
) (
	gonext bool, err error,
) {
	gonext = true
	for {
		// Max recursion depth is equal to len(topics) and limited by MaxCount.
		if pos >= uint8(len(topics)) {
			if rec == nil {
				return
			}

			var r *types.Log
			r, err = rec.FetchLog(tt.table.Logrec)
			if err != nil {
				return
			}
			gonext = onLog(r)
			return
		}
		if len(topics[pos]) < 1 {
			pos++
			continue
		}
		break
	}

	for _, variant := range topics[pos] {
		var (
			prefix  [topicKeySize]byte
			prefLen int
		)
		copy(prefix[prefLen:], variant.Bytes())
		prefLen += common.HashLength
		copy(prefix[prefLen:], posToBytes(pos))
		prefLen += uint8Size
		if rec != nil {
			copy(prefix[prefLen:], rec.ID.Bytes())
			prefLen += logrecKeySize
		}

		it := tt.table.Topic.NewIterator(prefix[:prefLen], blockStart)
		for it.Next() {
			id := extractLogrecID(it.Key())
			topicCount := bytesToPos(it.Value())
			newRec := newLogrec(id, topicCount)
			gonext, err = tt.walk(newRec, nil, topics, pos+1, onLog)
			if err != nil || !gonext {
				it.Release()
				return
			}
		}
		it.Release()
	}

	return
}
