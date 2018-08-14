package main

import (
	"errors"
	"fmt"
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

type feeRate float64

type txConfirmStatBucketCount struct {
	txCount float64
	feeSum  float64
}

type txConfirmStatBucket struct {
	upperBound   feeRate
	confirmed    []txConfirmStatBucketCount
	confirmCount float64
	feeSum       float64
}

type txConfirmStats struct {
	buckets     []txConfirmStatBucket
	memPool     []txConfirmStatBucket
	maxConfirms int32
	decay       float64
}

func newTxConfirmStats() *txConfirmStats {
	// some constants based on the original bitcoin core code
	maxConfirms := int32(5)
	decay := 0.998
	bucketFees := make([]feeRate, 0)
	for f := float64(10); f < 3e8; f *= 1.5 {
		bucketFees = append(bucketFees, feeRate(f))
	}

	nbBuckets := len(bucketFees)
	res := &txConfirmStats{
		buckets:     make([]txConfirmStatBucket, nbBuckets),
		memPool:     make([]txConfirmStatBucket, nbBuckets),
		maxConfirms: maxConfirms,
		decay:       decay,
	}

	for i, f := range bucketFees {
		res.buckets[i] = txConfirmStatBucket{
			upperBound: f,
			confirmed:  make([]txConfirmStatBucketCount, maxConfirms),
		}
		res.memPool[i] = txConfirmStatBucket{
			upperBound: f,
			confirmed:  make([]txConfirmStatBucketCount, maxConfirms),
		}
	}

	return res
}

func (stats *txConfirmStats) dumpBuckets() string {
	res := "           |"
	for c := 0; c < int(stats.maxConfirms); c++ {
		res += fmt.Sprintf(" %10d |", c)
	}
	res += "\n"

	l := len(stats.buckets)
	for i := 0; i < l-1; i++ {
		res += fmt.Sprintf("%.8f", stats.buckets[i].upperBound/1e8)
		for c := 0; c < int(stats.maxConfirms); c++ {
			avg := float64(0)
			if stats.buckets[i].confirmed[c].txCount > 0 {
				avg = stats.buckets[i].confirmed[c].feeSum / stats.buckets[i].confirmed[c].txCount / 1e8
			}

			res += " | " + fmt.Sprintf("%.8f", avg)
		}
		res += "\n"
	}

	return res
}

// lowerBucket returns the bucket that has the highest upperBound such that it
// is still lower than rate
func (stats *txConfirmStats) lowerBucket(rate feeRate) int32 {
	l := len(stats.buckets)
	for i := 0; i < l-1; i++ {
		if stats.buckets[i+1].upperBound > rate {
			return int32(i)
		}
	}
	return int32(l - 1)
}

// confirmRange returns the confirmation range bucket to be used for the given
// number of blocks to confirm
func (stats *txConfirmStats) confirmRange(blocksToConfirm int32) int32 {
	if blocksToConfirm >= stats.maxConfirms {
		return stats.maxConfirms - 1
	} else {
		return blocksToConfirm
	}
}

// updateMovingAverages updates the moving averages for the existing confirmed
// statistics and increases the confirmation ranges for mempool txs. This is
// meant to be called when a new block is mined, so that we discount older
// information.
func (stats *txConfirmStats) updateMovingAverages() {

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

		// and finally, the very first confirmation range (ie, what will enter
		// the mempool now that a new block has been mined) is zeroed so we can
		// start tracking brand new txs.
		bucket.confirmed[0].txCount = 0
		bucket.confirmed[0].feeSum = 0
	}
}

// newMemPoolTx records a new memPool transaction into the stats. A brand new
// mempool transaction has a minimum confirmation range of 1, so it is inserted
// into the very first confirmation range bucket of the appropriate fee rate
// bucket.
func (stats *txConfirmStats) newMemPoolTx(rate feeRate) {
	bucketIdx := stats.lowerBucket(rate)
	conf := &stats.memPool[bucketIdx].confirmed[0]
	conf.feeSum += float64(rate)
	conf.txCount++
}

// newMinedTx moves a mined tx from the mempool into the confirmed statistics.
// Note that this should only be called if the transaction had been seen and
// previously tracked by calling newMemPoolTx for it. Failing to observe that
// will result in undefined statistical results.
func (stats *txConfirmStats) newMinedTx(blocksToConfirm int32, rate feeRate) {
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

	// remove this same tx from the mempool as it has been mined
	beforeConf := stats.memPool[bucketIdx].confirmed[confirmIdx]
	bucket = &stats.memPool[bucketIdx]
	conf := &bucket.confirmed[confirmIdx]
	conf.feeSum -= float64(rate)
	conf.txCount--
	if conf.txCount < 0 {
		fmt.Println(bucket)
		fmt.Println(beforeConf)
		panic(fmt.Errorf("blabal %d ; %d ; %f", confirmIdx, blocksToConfirm, conf.txCount))
	}
}

func (stats *txConfirmStats) removeFromMemPool() {
	panic(fmt.Errorf("not yet. This is needed"))
}

// estimateMedianFee estimates the median fee rate for the current recorded
// statistics such that at least successPct transactions have been mined on all
// tracked fee rate buckets with fee >= to the median.
// In other words, this is the median fee of the lowest bucket such that it and
// all higher fee buckets have >= successPct transactions confirmed in at most
// `minConfs` confirmations.
// Note that sometimes the requested combination of minConfs and successPct is
// not achieveable (hypothetical example: 99% of txs confirmed within 1 block)
// or there are not enough recorded statistics to derive a successful estimate
// (eg: confirmation tracking has only started or there was a period of very few
// transactions). In those situations, the appropriate error is returned.
func (stats *txConfirmStats) estimateMedianFee(minConfs int32, successPct float64) (feeRate, error) {
	minTxCount := float64(1)

	startIdx := len(stats.buckets) - 1
	confirmRangeIdx := stats.confirmRange(minConfs)

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

		// fmt.Println("xxxx", b, totalTxs, confirmedTxs, stats.memPool[b].confirmed[confirmRangeIdx].txCount)

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
