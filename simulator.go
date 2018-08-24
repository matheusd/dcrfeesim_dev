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
	"container/heap"
	"fmt"
	"math"
	"math/rand"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

var (
	maxBlockPayload = uint32(chaincfg.MainNetParams.MaximumBlockSizes[0] -
		wire.MaxBlockHeaderPayload -
		5*421) // 5 votes
)

type histItem struct {
	value uint32
	count uint32
}

type simTx struct {
	size      uint32
	feeRate   uint32
	fee       uint32
	genHeight uint32
	txHash    chainhash.Hash
}

type txPool []*simTx

func (s txPool) Len() int            { return len(s) }
func (s txPool) Swap(i, j int)       { s[i], s[j] = s[j], s[i] }
func (s txPool) Less(i, j int) bool  { return s[i].feeRate > s[j].feeRate }
func (s *txPool) Push(x interface{}) { *s = append(*s, x.(*simTx)) }
func (s *txPool) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]
	return x
}

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

	// feeRateHistReportValues are the values to report in fee rate histogram
	// (automatically calculated if nil)
	feeRateHistReportValues []uint32
}

type simulator struct {
	cfg *simulatorConfig
	rnd *rand.Rand

	// histograms for raw generated data
	histBlockSize    []*histItem
	histTxSize       []*histItem
	histFeeRates     []*histItem
	histTxCount      []*histItem
	histTxMined      []*histItem
	mempoolFillCount int
	totalBlockCount  int
	longestMineDelay uint32
}

func newSimulator(cfg *simulatorConfig) *simulator {
	sim := &simulator{
		cfg: cfg,
		rnd: rand.New(rand.NewSource(0x1701d)),
	}

	// setup the vars that track histograms for the simulator (used to verify
	// whether the simulation is reasonable)
	for s := uint32(256); s < maxBlockPayload; s = uint32(float64(s) * 1.7) {
		sim.histBlockSize = append(sim.histBlockSize, &histItem{value: s})
		sim.histTxSize = append(sim.histTxSize, &histItem{value: s})
	}
	sim.histBlockSize = append(sim.histBlockSize, &histItem{value: maxBlockPayload + 1})
	sim.histTxSize = append(sim.histTxSize, &histItem{value: maxBlockPayload + 1})

	if len(cfg.feeRateHistReportValues) == 0 {
		minReportFee := float64(cfg.minimumFeeRate) * 0.75
		if minReportFee < 10 {
			minReportFee = 10
		}
		maxReportFee := (float64(cfg.minimumFeeRate) + cfg.feeRateCoef) * 15
		reportFeeStep := (maxReportFee - minReportFee) / 10
		for f := uint32(minReportFee); f < uint32(maxReportFee); f = uint32(float64(f) + reportFeeStep) {
			sim.histFeeRates = append(sim.histFeeRates, &histItem{value: f})
		}
	} else {
		sim.histFeeRates = make([]*histItem, len(cfg.feeRateHistReportValues))
		for i, v := range cfg.feeRateHistReportValues {
			sim.histFeeRates[i] = &histItem{value: v}
		}
	}

	for t := uint32(1); t < 5000; t = uint32(float64(t) * 2) {
		sim.histTxCount = append(sim.histTxCount, &histItem{value: t})
	}

	sim.histTxMined = []*histItem{
		&histItem{value: 1},
		&histItem{value: 2},
		&histItem{value: 3},
		&histItem{value: 4},
		&histItem{value: 6},
		&histItem{value: 10},
		&histItem{value: 16},
		&histItem{value: 32},
		&histItem{value: 64},
		&histItem{value: 0x7fffffff},
	}

	return sim
}

func (sim *simulator) genTransactions(currentHeight uint32, memPool *txPool) []*simTx {
	// value for number of txs per block and size of tx drawn from exponential
	// distributions eyeballed from charts. Improve this plzzz.

	// exponential distribution of number of txs per block interval.
	// 15.0 = very few full blocks. 60 = about 5% full blocks 125 = about 24%
	// full blocks 250 = about 50% of full blocks
	//nbTx := int(rnd.ExpFloat64() * 125.0)
	nbTx := int(sim.rnd.ExpFloat64() * sim.cfg.nbTxsCoef)

	txs := make([]*simTx, nbTx)
	startFee := sim.cfg.minimumFeeRate * 99 / 100
	for i := 0; i < nbTx; i++ {
		txs[i] = &simTx{
			size:      217 + uint32(sim.rnd.ExpFloat64()*sim.cfg.txSizeCoef),
			feeRate:   startFee + uint32(math.Floor(sim.rnd.ExpFloat64()*sim.cfg.feeRateCoef)), // atoms/KB
			genHeight: currentHeight,
		}
		if txs[i].feeRate < sim.cfg.minimumFeeRate {
			txs[i].feeRate = sim.cfg.minimumFeeRate
		}
		// fmt.Println("xxxxx", txs[i].feeRate)
		// panic(fmt.Errorf("xxxx"))
		if sim.rnd.Intn(10000) == 1 {
			// this is to add a few outlier big txs, otherwise the distribution
			// lacks those
			txs[i].size += 10000 * uint32(1+sim.rnd.Intn(3))
		}
		if txs[i].size > maxBlockPayload {
			txs[i].size = maxBlockPayload
		}
		txs[i].fee = txs[i].feeRate * txs[i].size / 1000
		txs[i].txHash[0] = byte(currentHeight >> 24)
		txs[i].txHash[1] = byte(currentHeight >> 16)
		txs[i].txHash[2] = byte(currentHeight >> 8)
		txs[i].txHash[3] = byte(currentHeight)
		txs[i].txHash[4] = byte(i >> 24)
		txs[i].txHash[5] = byte(i >> 16)
		txs[i].txHash[6] = byte(i >> 8)
		txs[i].txHash[7] = byte(i)
		heap.Push(memPool, txs[i])
	}

	return txs
}

// mineTransactions just mines txs up until the block is full
func (sim *simulator) mineTransactions(currentHeight uint32, memPool *txPool) []*simTx {
	sumSize := uint32(0)
	mined := make([]*simTx, 0)

	for memPool.Len() > 0 {
		tx := heap.Pop(memPool).(*simTx)
		if sumSize+tx.size > maxBlockPayload {
			heap.Push(memPool, tx)
			break
		}
		mined = append(mined, tx)
		sumSize += tx.size
		if currentHeight-tx.genHeight > sim.longestMineDelay {
			sim.longestMineDelay = currentHeight - tx.genHeight
		}
	}

	if memPool.Len() > 0 {
		sim.mempoolFillCount++
	}
	sim.totalBlockCount++

	return mined
}

func totalTxsSizes(txs []*simTx) uint32 {
	res := uint32(0)
	for _, tx := range txs {
		res += tx.size
	}
	return res
}

func (sim *simulator) trackHistograms(minedTxs []*simTx, newTxs []*simTx, currentHeight uint32) {
	blockSize := totalTxsSizes(minedTxs)
	for h := 1; h < len(sim.histBlockSize); h++ {
		if sim.histBlockSize[h].value > blockSize {
			sim.histBlockSize[h-1].count++
			break
		}
	}

	numTx := uint32(len(minedTxs))
	for h := 1; h < len(sim.histTxCount); h++ {
		if sim.histTxCount[h].value > numTx {
			sim.histTxCount[h-1].count++
			break
		}
	}

	for _, tx := range minedTxs {
		mineDelay := currentHeight - tx.genHeight
		for h := 0; h < len(sim.histTxMined); h++ {
			if mineDelay <= sim.histTxMined[h].value {
				sim.histTxMined[h].count++
				break
			}
		}
	}

	for _, tx := range newTxs {
		for h := 1; h < len(sim.histTxSize); h++ {
			if sim.histTxSize[h].value > tx.size {
				sim.histTxSize[h-1].count++
				break
			}
		}
		for h := 1; h < len(sim.histFeeRates); h++ {
			if sim.histFeeRates[h].value > tx.feeRate {
				sim.histFeeRates[h-1].count++
				break
			}
		}

	}
}

func (sim *simulator) reportSimHistograms() {

	countHistItems := func(items []*histItem) float64 {
		res := float64(0)
		for _, i := range items {
			res += float64(i.count)
		}
		return res
	}

	l1 := ""
	l2 := ""
	l3 := ""
	tot := countHistItems(sim.histBlockSize)
	for _, h := range sim.histBlockSize {
		l1 += fmt.Sprintf("%8.2f", float64(h.value)/1000.0)
		l2 += fmt.Sprintf("%8d", h.count)
		l3 += fmt.Sprintf("%8.2f", float64(h.count)/tot*100)
	}
	fmt.Printf("Block Size Histogram\n%s\n%s\n%s\n", l1, l2, l3)

	l1 = ""
	l2 = ""
	l3 = ""
	tot = countHistItems(sim.histTxSize)
	for _, h := range sim.histTxSize {
		l1 += fmt.Sprintf("%8.2f", float64(h.value)/1000.0)
		l2 += fmt.Sprintf("%8d", h.count)
		l3 += fmt.Sprintf("%8.2f", float64(h.count)/tot*100)
	}
	fmt.Printf("\nTx Size Histogram\n%s\n%s\n%s\n", l1, l2, l3)

	l1 = ""
	l2 = ""
	l3 = ""
	tot = countHistItems(sim.histFeeRates)
	for _, h := range sim.histFeeRates {
		l1 += fmt.Sprintf("%13.8f", float64(h.value)/1e8)
		l2 += fmt.Sprintf("%13d", h.count)
		l3 += fmt.Sprintf("%13.2f", float64(h.count)/tot*100)
	}
	fmt.Printf("\nFee Rate Histogram\n%s\n%s\n%s\n", l1, l2, l3)

	l1 = ""
	l2 = ""
	l3 = ""
	tot = countHistItems(sim.histTxCount)
	for _, h := range sim.histTxCount {
		l1 += fmt.Sprintf("%8d", h.value)
		l2 += fmt.Sprintf("%8d", h.count)
		l3 += fmt.Sprintf("%8.2f", float64(h.count)/tot*100)
	}
	fmt.Printf("\nTx per block Histogram\n%s\n%s\n%s\n", l1, l2, l3)

	l1 = ""
	l2 = ""
	l3 = ""
	tot = countHistItems(sim.histTxMined)
	for _, h := range sim.histTxMined {
		l1 += fmt.Sprintf("%11d", h.value)
		l2 += fmt.Sprintf("%11d", h.count)
		l3 += fmt.Sprintf("%11.2f", float64(h.count)/tot*100)
	}
	fmt.Printf("\nMining Interval Histogram\n%s\n%s\n%s\n", l1, l2, l3)

	fmt.Printf("\nBlock Counts\n")
	fmt.Printf("  total = %d  w/ filled mempool = %d (%.2f%%)  longest mine "+
		"delay = %d\n",
		sim.totalBlockCount, sim.mempoolFillCount, float64(sim.mempoolFillCount)*100.0/
			float64(sim.totalBlockCount), sim.longestMineDelay)

	fmt.Println("")
}
