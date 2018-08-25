package main

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
)

var (
	// ErrNoSuccessPctBucketFound is the error returned when no bucket has been
	// found with the minimum required percentage success.
	ErrNoSuccessPctBucketFound = errors.New("no bucket with the minimum required " +
		"success percentage found")

	// ErrNotEnoughTxsForEstimate is the error returned when not enough
	// transactions have been seen by the fee generator to give an estimate
	ErrNotEnoughTxsForEstimate = errors.New("not enough transactions seen for " +
		"estimation")
)

// ErrTargetConfTooLarge is the type of error returned when an user of the
// estimator requested a confirmation range higher than tracked by the estimator.
type ErrTargetConfTooLarge struct {
	MaxConfirms int32
	ReqConfirms int32
}

func (e ErrTargetConfTooLarge) Error() string {
	return fmt.Sprintf("target confirmation requested (%d) higher than maximum"+
		"confirmation range tracked by estimator (%d)", e.ReqConfirms,
		e.MaxConfirms)
}

type feeRate float64

type txConfirmStatBucketCount struct {
	txCount float64
	feeSum  float64
}

type txConfirmStatBucket struct {
	confirmed    []txConfirmStatBucketCount
	confirmCount float64
	feeSum       float64
}

// EstimatorConfig stores the configuration parameters for a given fee
// estimator. It is used to initialize an empty fee estimator.
type EstimatorConfig struct {
	// MaxConfirms is the maximum number of confirmation ranges to check
	MaxConfirms uint32

	// MinBucketFee is the value of the fee of the lowest bucket for which
	// estimation is tracked
	MinBucketFee uint32

	// MaxBucketFee is the value of the fee for the highest bucket for which
	// estimation is tracked
	MaxBucketFee uint32

	// FeeRateStep is the multiplier to generate the fee rate buckets (each
	// bucket is higher than the previous one by this factor)
	FeeRateStep float64
}

// memPoolTxDesc is an aux structure used to track the local estimator mempool
type memPoolTxDesc struct {
	addedHeight int64
	bucketIndex int32
	fees        feeRate
}

// FeeEstimator tracks historical data for published and mined transactions in
// order to estimate fees to be used in new transactions for confirmation
// within a target block window.
type FeeEstimator struct {
	bucketFeeBounds []feeRate
	buckets         []txConfirmStatBucket
	memPool         []txConfirmStatBucket
	maxConfirms     int32
	decay           float64
	bestHeight      int64
	memPoolTxs      map[chainhash.Hash]memPoolTxDesc
}

// NewFeeEstimator returns an empty estimator given a config. This estimator
// then needs to be fed data for published and mined transactions before it can
// be used to estimate fees for new transactions.
func NewFeeEstimator(cfg *EstimatorConfig) *FeeEstimator {
	// some constants based on the original bitcoin core code
	decay := 0.998
	maxConfirms := cfg.MaxConfirms
	bucketFees := make([]feeRate, 0)
	max := float64(cfg.MaxBucketFee)
	for f := float64(cfg.MinBucketFee); f < max; f *= cfg.FeeRateStep {
		bucketFees = append(bucketFees, feeRate(f))
	}

	// The last bucket catches everything else, so it uses an upper bound of
	// +inf which any rate must be lower than
	bucketFees = append(bucketFees, feeRate(math.Inf(1)))

	nbBuckets := len(bucketFees)
	res := &FeeEstimator{
		bucketFeeBounds: bucketFees,
		buckets:         make([]txConfirmStatBucket, nbBuckets),
		memPool:         make([]txConfirmStatBucket, nbBuckets),
		maxConfirms:     int32(maxConfirms),
		decay:           decay,
		memPoolTxs:      make(map[chainhash.Hash]memPoolTxDesc),
	}

	for i := range bucketFees {
		res.buckets[i] = txConfirmStatBucket{
			confirmed: make([]txConfirmStatBucketCount, maxConfirms),
		}
		res.memPool[i] = txConfirmStatBucket{
			confirmed: make([]txConfirmStatBucketCount, maxConfirms),
		}
	}

	return res
}

// dumpBuckets returns the internal estimator state as a string
func (stats *FeeEstimator) dumpBuckets() string {
	res := "          |"
	for c := 0; c < int(stats.maxConfirms); c++ {
		if c == int(stats.maxConfirms)-1 {
			res += fmt.Sprintf("   %14s", "+Inf")
		} else {
			res += fmt.Sprintf("   %14d|", c+1)
		}
	}
	res += "\n"

	l := len(stats.bucketFeeBounds)
	for i := 0; i < l; i++ {
		res += fmt.Sprintf("%10.8f", stats.bucketFeeBounds[i]/1e8)
		for c := 0; c < int(stats.maxConfirms); c++ {
			avg := float64(0)
			count := stats.buckets[i].confirmed[c].txCount
			if stats.buckets[i].confirmed[c].txCount > 0 {
				avg = stats.buckets[i].confirmed[c].feeSum / stats.buckets[i].confirmed[c].txCount / 1e8
			}

			res += fmt.Sprintf("| %.8f %5.0f", avg, count)
		}
		res += "\n"
	}

	return res
}

// lowerBucket returns the bucket that has the highest upperBound such that it
// is still lower than rate
func (stats *FeeEstimator) lowerBucket(rate feeRate) int32 {
	res := sort.Search(len(stats.bucketFeeBounds), func(i int) bool {
		return stats.bucketFeeBounds[i] >= rate
	})
	return int32(res)
}

// confirmRange returns the confirmation range index to be used for the given
// number of blocks to confirm. The last confirmation range has an upper bound
// of +inf to mean that it represents all confirmations higher than the second
// to last bucket.
func (stats *FeeEstimator) confirmRange(blocksToConfirm int32) int32 {
	idx := blocksToConfirm - 1
	if idx >= stats.maxConfirms {
		return stats.maxConfirms - 1
	}
	return idx
}

// updateMovingAverages updates the moving averages for the existing confirmed
// statistics and increases the confirmation ranges for mempool txs. This is
// meant to be called when a new block is mined, so that we discount older
// information.
func (stats *FeeEstimator) updateMovingAverages(newHeight int64) {

	// decay the existing stats so that, over time, we rely on more up to date
	// information regarding fees.
	for b := 0; b < len(stats.buckets); b++ {
		bucket := &stats.buckets[b]
		bucket.feeSum *= stats.decay
		bucket.confirmCount *= stats.decay
		for c := 0; c < len(bucket.confirmed); c++ {
			conf := &bucket.confirmed[c]
			conf.feeSum *= stats.decay
			conf.txCount *= stats.decay
		}
	}

	// For unconfirmed (mempool) transactions, every transaction will now take
	// at least one additional block to confirm. So for every fee bucket, we
	// move the stats up one confirmation range.
	for b := 0; b < len(stats.memPool); b++ {
		bucket := &stats.memPool[b]

		// the last confirmation range represents all txs confirmed at >= than
		// the initial maxConfirms, so we *add* the second to last range into
		// the last range
		c := len(bucket.confirmed) - 1
		bucket.confirmed[c].txCount += bucket.confirmed[c-1].txCount
		bucket.confirmed[c].feeSum += bucket.confirmed[c-1].feeSum

		// for the other ranges, just move up the stats
		for c--; c > 0; c-- {
			bucket.confirmed[c] = bucket.confirmed[c-1]
		}
		// copy(bucket.confirmed[1:c-1], bucket.confirmed[2:c-1])z

		// and finally, the very first confirmation range (ie, what will enter
		// the mempool now that a new block has been mined) is zeroed so we can
		// start tracking brand new txs.
		bucket.confirmed[0].txCount = 0
		bucket.confirmed[0].feeSum = 0
	}

	stats.bestHeight = newHeight
}

// newMemPoolTx records a new memPool transaction into the stats. A brand new
// mempool transaction has a minimum confirmation range of 1, so it is inserted
// into the very first confirmation range bucket of the appropriate fee rate
// bucket.
func (stats *FeeEstimator) newMemPoolTx(bucketIdx int32, fees feeRate) {
	conf := &stats.memPool[bucketIdx].confirmed[0]
	conf.feeSum += float64(fees)
	conf.txCount++
}

// newMinedTx moves a mined tx from the mempool into the confirmed statistics.
// Note that this should only be called if the transaction had been seen and
// previously tracked by calling newMemPoolTx for it. Failing to observe that
// will result in undefined statistical results.
func (stats *FeeEstimator) newMinedTx(blocksToConfirm int32, rate feeRate) {
	bucketIdx := stats.lowerBucket(rate)
	confirmIdx := stats.confirmRange(blocksToConfirm)
	bucket := &stats.buckets[bucketIdx]

	// increase the counts for all confirmation ranges starting at the first
	// confirmIdx because it took at least `blocksToConfirm` for this tx to be
	// mined. This is used to simplify the bucket selection during estimation,
	// so that we only need to check a single confirmation range (instead of
	// iterating to sum all confirmations with <= `minConfs`).
	for c := int(confirmIdx); c < len(bucket.confirmed); c++ {
		conf := &bucket.confirmed[c]
		conf.feeSum += float64(rate)
		conf.txCount++
	}
	bucket.confirmCount++
	bucket.feeSum += float64(rate)
}

func (stats *FeeEstimator) removeFromMemPool(blocksInMemPool int32, rate feeRate) {
	bucketIdx := stats.lowerBucket(rate)
	confirmIdx := stats.confirmRange(blocksInMemPool + 1)
	beforeConf := stats.memPool[bucketIdx].confirmed[confirmIdx]
	bucket := &stats.memPool[bucketIdx]
	conf := &bucket.confirmed[confirmIdx]
	conf.feeSum -= float64(rate)
	conf.txCount--
	if conf.txCount < 0 {
		// if this happens, it means a transaction has been called on this
		// function but not on a previous newMemPoolTx.
		fmt.Println(bucket)
		fmt.Println(beforeConf)
		panic(fmt.Errorf("wrong usage %d ; %d ; %f", confirmIdx, blocksInMemPool, conf.txCount))
	}
}

// estimateMedianFee estimates the median fee rate for the current recorded
// statistics such that at least successPct transactions have been mined on all
// tracked fee rate buckets with fee >= to the median.
// In other words, this is the median fee of the lowest bucket such that it and
// all higher fee buckets have >= successPct transactions confirmed in at most
// `targetConfs` confirmations.
// Note that sometimes the requested combination of targetConfs and successPct is
// not achieveable (hypothetical example: 99% of txs confirmed within 1 block)
// or there are not enough recorded statistics to derive a successful estimate
// (eg: confirmation tracking has only started or there was a period of very few
// transactions). In those situations, the appropriate error is returned.
func (stats *FeeEstimator) estimateMedianFee(targetConfs int32, successPct float64) (feeRate, error) {
	minTxCount := float64(1)

	if (targetConfs - 1) >= stats.maxConfirms {
		// We might want to add support to use a targetConf at +infinity to
		// allow us to make estimates at confirmation interval higher than what
		// we currently track.
		return 0, ErrTargetConfTooLarge{MaxConfirms: stats.maxConfirms,
			ReqConfirms: targetConfs}
	}

	startIdx := len(stats.buckets) - 1
	confirmRangeIdx := stats.confirmRange(targetConfs)

	var totalTxs, confirmedTxs float64
	bestBucketsStt := startIdx
	bestBucketsEnd := startIdx
	curBucketsStt := startIdx
	curBucketsEnd := startIdx

	for b := startIdx; b >= 0; b-- {
		totalTxs += stats.buckets[b].confirmCount
		confirmedTxs += stats.buckets[b].confirmed[confirmRangeIdx].txCount

		// add the mempool (unconfirmed) transactions to the total tx count
		// since a very large mempool for the given bucket might mean that
		// miners are reluctant to include these in their mined blocks
		totalTxs += stats.memPool[b].confirmed[confirmRangeIdx].txCount

		curBucketsStt = b
		if totalTxs > minTxCount {
			if confirmedTxs/totalTxs < successPct {
				if curBucketsEnd == startIdx {
					return 0, ErrNoSuccessPctBucketFound
				}
				break
			}

			bestBucketsStt = curBucketsStt
			bestBucketsEnd = curBucketsEnd
			curBucketsEnd = b - 1
			totalTxs = 0
			confirmedTxs = 0
		}
	}

	txCount := float64(0)
	for b := bestBucketsStt; b <= bestBucketsEnd; b++ {
		txCount += stats.buckets[b].confirmCount
	}
	if txCount <= 0 {
		return 0, ErrNotEnoughTxsForEstimate
	}
	txCount = txCount / 2
	for b := bestBucketsStt; b <= bestBucketsEnd; b++ {
		if stats.buckets[b].confirmCount < txCount {
			txCount -= stats.buckets[b].confirmCount
		} else {
			median := stats.buckets[b].feeSum / stats.buckets[b].confirmCount
			return feeRate(median), nil
		}
	}

	return 0, errors.New("this isn't supposed to be reached")
}

// AddMemPoolTransaction to the estimator in order to account for it in the
// estimations. It assumes that this transaction is entering the mempool at the
// currently recorded best chain hash, using the total fee amount (in atoms) and
// with the provided size (in bytes).
func (stats *FeeEstimator) AddMemPoolTransaction(txHash *chainhash.Hash, fee, size int64) {

	// TODO: add lock

	if _, exists := stats.memPoolTxs[*txHash]; exists {
		// we should not double count transactions
		return
	}

	rate := feeRate(fee / size * 1000)

	if rate < stats.bucketFeeBounds[0] {
		// Transactions paying less than the current relaying fee can only
		// possibly be included in the high priority/zero fee area of blocks,
		// which are usually of limited size, so we explicitly don't track those.
		// On decred, this also naturally handles votes (SSGen transactions)
		// which don't carry a tx fee and are required for inclusion in blocks.
		// Note that the test is explicitly < instead of <= so that we *can*
		// track transactions pay *exactly* the minimum fee.
		return
	}

	tx := memPoolTxDesc{
		addedHeight: stats.bestHeight,
		bucketIndex: stats.lowerBucket(rate),
		fees:        rate,
	}
	stats.memPoolTxs[*txHash] = tx
	stats.newMemPoolTx(tx.bucketIndex, rate)
}

// RemoveMemPoolTransaction from statistics tracking.
func (stats *FeeEstimator) RemoveMemPoolTransaction(txHash *chainhash.Hash) {

	// TODO: add lock
	desc, exists := stats.memPoolTxs[*txHash]
	if !exists {
		// we were not previously tracking this, so no need to remove
		return
	}

	stats.removeFromMemPool(int32(stats.bestHeight-desc.addedHeight), desc.fees)
	delete(stats.memPoolTxs, *txHash)
	return
}

// ProcessMinedTransactions moves the transactions that exist in the currently
// tracked mempool into a mined state.
func (stats *FeeEstimator) ProcessMinedTransactions(blockHeight int64, txHashes []*chainhash.Hash) {

	// TODO: add lock

	if blockHeight <= stats.bestHeight {
		// we don't explicitly track reorgs right now
		return
	}

	stats.updateMovingAverages(blockHeight)

	for _, txh := range txHashes {
		desc, exists := stats.memPoolTxs[*txh]
		if !exists {
			// we cannot use transactions that we didn't know about to estimate
			// because that opens up the possibility of miners introducing
			// dummy, high fee transactions which would tend to then increase
			// the average fee estimate.
			// Tracking only previously known transactions force miners trying
			// to pull this attack to broadcast their transactions and possibly
			// forfeit their coins by having the transaction mined by a
			// competitor
			continue
		}

		stats.removeFromMemPool(int32(blockHeight-desc.addedHeight), desc.fees)
		delete(stats.memPoolTxs, *txh)

		if blockHeight <= desc.addedHeight {
			// this shouldn't usually happen but we need to explicitly test for
			// because we can't account for non positive confirmation ranges in
			// mined transactions.
			continue
		}

		stats.newMinedTx(int32(blockHeight-desc.addedHeight), desc.fees)
	}
}

// ProcessBlock processes all mined transactions in the provided block
func (stats *FeeEstimator) ProcessBlock(block *dcrutil.Block) {
	txs := make([]*chainhash.Hash, len(block.Transactions())+
		len(block.STransactions()))
	i := 0
	for _, tx := range block.Transactions() {
		txs[i] = tx.Hash()
		i++
	}
	for _, tx := range block.STransactions() {
		txs[i] = tx.Hash()
		i++
	}

	stats.ProcessMinedTransactions(block.Height(), txs)
}
