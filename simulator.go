package main

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
)

var (
	rnd             = rand.New(rand.NewSource(0x1701d))
	maxBlockPayload = uint32(chaincfg.MainNetParams.MaximumBlockSizes[0] -
		wire.MaxBlockHeaderPayload -
		5*421) // 5 votes
	histBlockSize []*histBlockSizeItem
	histTxSize    []*histTxSizeItem
	histFeeRates  []*histFeeRateItem
	histTxCount   []*histTxCountItem
)

type histBlockSizeItem struct {
	size  uint32
	count uint32
}

type histTxSizeItem struct {
	size  uint32
	count uint32
}

type histFeeRateItem struct {
	feeRate uint32
	count   uint32
}

type histTxCountItem struct {
	txPerBlock uint32
	count      uint32
}

type simTx struct {
	size      uint32
	feeRate   uint32
	genHeight uint32
}

type simTxsByFeeRate []*simTx

func (s simTxsByFeeRate) Len() int           { return len(s) }
func (s simTxsByFeeRate) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s simTxsByFeeRate) Less(i, j int) bool { return s[i].feeRate > s[j].feeRate }

func simGenTxs(currentHeight uint32) []*simTx {
	// value for number of txs per block and size of tx drawn from exponential
	// distributions eyeballed from charts. Improve this plzzz.

	// exponential distribution of number of txs per block interval.
	// 15.0 = very few full blocks. 60 = about 5% full blocks 125 = about 24%
	// full blocks 250 = about 50% of full blocks
	nbTx := int(rnd.ExpFloat64() * 125.0)

	txs := make([]*simTx, nbTx)
	for i := 0; i < nbTx; i++ {
		txs[i] = &simTx{
			size:      217 + uint32(rnd.ExpFloat64()*1000.0),
			feeRate:   1e5, // 0.001 dcr/KB
			genHeight: currentHeight,
		}
		if rnd.Intn(10000) == 1 {
			// this is to add a few outlier big txs, otherwise the distribution
			// lacks those
			txs[i].size += 10000 * uint32(1+rnd.Intn(3))
		}
		if txs[i].size > maxBlockPayload {
			txs[i].size = maxBlockPayload
		}
	}
	return txs
}

// trivialMiner just mines txs up until the block is full
func trivialMiner(memPool []*simTx) ([]*simTx, []*simTx) {
	sort.Sort(simTxsByFeeRate(memPool))

	sumSize := uint32(0)
	mined := make([]*simTx, 0, len(memPool))
	var newMemPool []*simTx

	for i, tx := range memPool {
		if sumSize+tx.size > maxBlockPayload {
			newMemPool = memPool[i:]
			break
		}
		mined = append(mined, tx)
		sumSize += tx.size
	}

	return mined, newMemPool
}

func totalTxsSizes(txs []*simTx) uint32 {
	res := uint32(0)
	for _, tx := range txs {
		res += tx.size
	}
	return res
}

func trackSimHistograms(minedTxs []*simTx, newTxs []*simTx) {
	blockSize := totalTxsSizes(minedTxs)
	for h := 1; h < len(histBlockSize); h++ {
		if histBlockSize[h].size > blockSize {
			histBlockSize[h-1].count++
			break
		}
	}

	numTx := uint32(len(minedTxs))
	for h := 1; h < len(histTxCount); h++ {
		if histTxCount[h].txPerBlock > numTx {
			histTxCount[h-1].count++
			break
		}
	}

	for _, tx := range newTxs {
		for h := 1; h < len(histTxSize); h++ {
			if histTxSize[h].size > tx.size {
				histTxSize[h-1].count++
				break
			}
		}
		for h := 1; h < len(histFeeRates); h++ {
			if histFeeRates[h].feeRate > tx.feeRate {
				histFeeRates[h].count++
				break
			}
		}
	}
}

func reportSimHistograms() {
	l1 := ""
	l2 := ""
	for _, h := range histBlockSize {
		l1 += fmt.Sprintf("%8.2f", float64(h.size)/1000.0)
		l2 += fmt.Sprintf("%8d", h.count)
	}
	fmt.Printf("\nBlock Size Histogram\n%s\n%s\n", l1, l2)

	l1 = ""
	l2 = ""
	for _, h := range histTxSize {
		l1 += fmt.Sprintf("%8.2f", float64(h.size)/1000.0)
		l2 += fmt.Sprintf("%8d", h.count)
	}
	fmt.Printf("\nTx Size Histogram\n%s\n%s\n", l1, l2)

	l1 = ""
	l2 = ""
	for _, h := range histFeeRates {
		l1 += fmt.Sprintf("%10.5f", float64(h.feeRate)/1e8)
		l2 += fmt.Sprintf("%10d", h.count)
	}
	fmt.Printf("\nFee Rate Histogram\n%s\n%s\n", l1, l2)

	l1 = ""
	l2 = ""
	for _, h := range histTxCount {
		l1 += fmt.Sprintf("%6d", h.txPerBlock)
		l2 += fmt.Sprintf("%6d", h.count)
	}
	fmt.Printf("\nTx per block Histogram\n%s\n%s\n", l1, l2)
}
