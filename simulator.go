// Simulator module. This generates new transactions and mines them in a
// distribution attempting to mimic real world conditions.
//
// Right now it's not super accurate; it's only been eyeballed by a generated
// set of histograms to ensure the distribution of published/mined transactions
// is reasonable.
//
// To adjust the rate and size of generated transactions, modify the `simGenTxs`
// function.
// To adjust mining behavior, modify the `simMine` function.
package main

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
)

var (
	maxBlockPayload = uint32(chaincfg.MainNetParams.MaximumBlockSizes[0] -
		wire.MaxBlockHeaderPayload -
		5*421) // 5 votes
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

type simulatorConfig struct {
	// nbTxsCoef is the coefficient for the distribution of new transactions per
	// block
	nbTxsCoef float64

	// txSizeCoef is the coefficient for the distribution of transaction size
	// for new transactions
	txSizeCoef float64

	// minimumFeeRate is the minimum fee rate to use when generating a new
	// transaction (in atoms/KB)
	minimumFeeRate uint32

	// feeRateCoef is the coefficient for the distribution of fee rates for new
	// transactions
	feeRateCoef float64
}

type simulator struct {
	cfg *simulatorConfig
	rnd *rand.Rand

	// histograms for raw generated data
	histBlockSize       []*histBlockSizeItem
	histTxSize          []*histTxSizeItem
	histFeeRates        []*histFeeRateItem
	histTxCount         []*histTxCountItem
	mempoolFillCount    int
	totalBlockCount     int
	longestMineDelay    uint32
	longestUnminedDelay uint32
}

func newSimulator(cfg *simulatorConfig) *simulator {
	sim := &simulator{
		cfg: cfg,
		rnd: rand.New(rand.NewSource(0x1701d)),
	}

	// setup the vars that track histograms for the simulator (used to verify
	// whether the simulation is reasonable)
	for s := uint32(256); s < maxBlockPayload; s = uint32(float64(s) * 1.7) {
		sim.histBlockSize = append(sim.histBlockSize, &histBlockSizeItem{size: s})
		sim.histTxSize = append(sim.histTxSize, &histTxSizeItem{size: s})
	}
	sim.histBlockSize = append(sim.histBlockSize, &histBlockSizeItem{size: maxBlockPayload + 1})
	sim.histTxSize = append(sim.histTxSize, &histTxSizeItem{size: maxBlockPayload + 1})

	minReportFee := float64(cfg.minimumFeeRate) * 0.75
	if minReportFee < 10 {
		minReportFee = 10
	}
	maxReportFee := (float64(cfg.minimumFeeRate) + cfg.feeRateCoef) * 15
	reportFeeStep := (maxReportFee - minReportFee) / 10
	for f := uint32(minReportFee); f < uint32(maxReportFee); f = uint32(float64(f) + reportFeeStep) {
		sim.histFeeRates = append(sim.histFeeRates, &histFeeRateItem{feeRate: f})
	}

	for t := uint32(1); t < 5000; t = uint32(float64(t) * 2) {
		sim.histTxCount = append(sim.histTxCount, &histTxCountItem{txPerBlock: t})
	}

	return sim
}

func (sim *simulator) genTransactions(currentHeight uint32) []*simTx {
	// value for number of txs per block and size of tx drawn from exponential
	// distributions eyeballed from charts. Improve this plzzz.

	// exponential distribution of number of txs per block interval.
	// 15.0 = very few full blocks. 60 = about 5% full blocks 125 = about 24%
	// full blocks 250 = about 50% of full blocks
	//nbTx := int(rnd.ExpFloat64() * 125.0)
	nbTx := int(sim.rnd.ExpFloat64() * sim.cfg.nbTxsCoef)

	txs := make([]*simTx, nbTx)
	for i := 0; i < nbTx; i++ {
		txs[i] = &simTx{
			size:      217 + uint32(sim.rnd.ExpFloat64()*sim.cfg.txSizeCoef),
			feeRate:   sim.cfg.minimumFeeRate + uint32(sim.rnd.ExpFloat64()*sim.cfg.feeRateCoef), // atoms/KB
			genHeight: currentHeight,
		}
		if sim.rnd.Intn(10000) == 1 {
			// this is to add a few outlier big txs, otherwise the distribution
			// lacks those
			txs[i].size += 10000 * uint32(1+sim.rnd.Intn(3))
		}
		if txs[i].size > maxBlockPayload {
			txs[i].size = maxBlockPayload
		}
	}
	return txs
}

// mineTransactions just mines txs up until the block is full
func (sim *simulator) mineTransactions(currentHeight uint32, memPool []*simTx) ([]*simTx, []*simTx) {
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
		if currentHeight-tx.genHeight > sim.longestMineDelay {
			sim.longestMineDelay = currentHeight - tx.genHeight
		}
	}

	if len(newMemPool) > 0 {
		sim.mempoolFillCount++
		for _, tx := range newMemPool {
			if currentHeight-tx.genHeight > sim.longestUnminedDelay {
				sim.longestUnminedDelay = currentHeight - tx.genHeight
			}
		}
	}
	sim.totalBlockCount++

	return mined, newMemPool
}

func totalTxsSizes(txs []*simTx) uint32 {
	res := uint32(0)
	for _, tx := range txs {
		res += tx.size
	}
	return res
}

func (sim *simulator) trackHistograms(minedTxs []*simTx, newTxs []*simTx) {
	blockSize := totalTxsSizes(minedTxs)
	for h := 1; h < len(sim.histBlockSize); h++ {
		if sim.histBlockSize[h].size > blockSize {
			sim.histBlockSize[h-1].count++
			break
		}
	}

	numTx := uint32(len(minedTxs))
	for h := 1; h < len(sim.histTxCount); h++ {
		if sim.histTxCount[h].txPerBlock > numTx {
			sim.histTxCount[h-1].count++
			break
		}
	}

	for _, tx := range newTxs {
		for h := 1; h < len(sim.histTxSize); h++ {
			if sim.histTxSize[h].size > tx.size {
				sim.histTxSize[h-1].count++
				break
			}
		}
		for h := 1; h < len(sim.histFeeRates); h++ {
			if sim.histFeeRates[h].feeRate > tx.feeRate {
				sim.histFeeRates[h].count++
				break
			}
		}
	}
}

func (sim *simulator) reportSimHistograms() {
	l1 := ""
	l2 := ""
	for _, h := range sim.histBlockSize {
		l1 += fmt.Sprintf("%8.2f", float64(h.size)/1000.0)
		l2 += fmt.Sprintf("%8d", h.count)
	}
	fmt.Printf("Block Size Histogram\n%s\n%s\n", l1, l2)

	l1 = ""
	l2 = ""
	for _, h := range sim.histTxSize {
		l1 += fmt.Sprintf("%8.2f", float64(h.size)/1000.0)
		l2 += fmt.Sprintf("%8d", h.count)
	}
	fmt.Printf("\nTx Size Histogram\n%s\n%s\n", l1, l2)

	l1 = ""
	l2 = ""
	for _, h := range sim.histFeeRates {
		l1 += fmt.Sprintf("%10.5f", float64(h.feeRate)/1e8)
		l2 += fmt.Sprintf("%10d", h.count)
	}
	fmt.Printf("\nFee Rate Histogram\n%s\n%s\n", l1, l2)

	l1 = ""
	l2 = ""
	for _, h := range sim.histTxCount {
		l1 += fmt.Sprintf("%6d", h.txPerBlock)
		l2 += fmt.Sprintf("%6d", h.count)
	}
	fmt.Printf("\nTx per block Histogram\n%s\n%s\n", l1, l2)

	fmt.Printf("\nBlock Counts\n")
	fmt.Printf("  total = %d  w/ filled mempool = %d (%.2f%%)  longest mine "+
		"delay = %d  longest unmined delay = %d\n",
		sim.totalBlockCount, sim.mempoolFillCount, float64(sim.mempoolFillCount)*100.0/
			float64(sim.totalBlockCount), sim.longestMineDelay,
			sim.longestUnminedDelay)

	fmt.Println("")
}
