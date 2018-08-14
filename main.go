package main

import "fmt"

func main() {
	// How long to run the simulation of blocks before trying to estimate the
	// fees
	lenSimulation := uint32(288 * 30 * 3)

	simSetup()
	var memPool, newTxs, minedTxs []*simTx
	estimator := newTxConfirmStats()

	// simulate a bunch of blocks. At every iteration, this simulates:
	// - a miner generating a new block from the current memPool
	// - some new transactions appearing in the network and being added to the
	// outstanding mempool
	for h := uint32(1); h < lenSimulation; h++ {
		minedTxs, memPool = simMine(memPool)
		newTxs = simGenTxs(h)
		memPool = append(memPool, newTxs...)
		simTrackHistograms(minedTxs, newTxs)

		// Update the estimator (this is thing that would actually run in the
		// mempool of a full node once a new block has been fonud)
		estimator.updateMovingAverages()
		for _, tx := range minedTxs {
			estimator.newMinedTx(int32(h-tx.genHeight), feeRate(tx.feeRate))
		}

		// This would happen as new transactions are entering the memPool
		for _, tx := range newTxs {
			estimator.newMemPoolTx(feeRate(tx.feeRate))
		}
	}

	// Simulation has ended (eg: full node has synced)
	// Let's now try to estimate the fees.

	// Let's try generating fee rate estimates for a number of different minConf
	// amounts at the same success pct (this is roughly what bitcoin core does)
	fmt.Println("=== Fees to use for minConf confirmations ===")
	testMinConfs := []int32{1, 2, 4, 6, 8, 100}
	successPct := 0.95
	l1 := ""
	l2 := ""
	for _, t := range testMinConfs {
		l1 += fmt.Sprintf("%12d", t)
		fee, err := estimator.estimateMedianFee(t, successPct)
		if err != nil {
			if err == ErrNoSuccessPctBucketFound {
				l2 += "   noSuccBkt"
			} else if err == ErrNotEnoughTxsForEstimate {
				l2 += "   notEnghTx"
			} else {
				l2 += "         err"
			}
		} else {
			l2 += fmt.Sprintf("%12.8f", fee/1e8)
		}
	}
	fmt.Printf("%s\n%s\n\n", l1, l2)

	// Let's see the internal state of the estimator
	fmt.Println("=== Internal Estimator State ===")
	fmt.Println(estimator.dumpBuckets())

	// report the histogram of the simulated transactions to see if they are
	// reasonable
	fmt.Println("=== Histograms for simulated data ===")
	reportSimHistograms()
}
