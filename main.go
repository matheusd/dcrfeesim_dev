package main

func main() {
	lenSimulation := uint32(288 * 30 * 12)
	// testMinConfs := []int32{1, 2, 5, 10, 24, 100}
	// successPct := 0.95

	// setup the vars that track histograms for the simulator (used to verify
	// whether the simulation is reasonable)
	for s := uint32(256); s < maxBlockPayload; s = uint32(float64(s) * 1.7) {
		histBlockSize = append(histBlockSize, &histBlockSizeItem{size: s})
		histTxSize = append(histTxSize, &histTxSizeItem{size: s})
	}
	histBlockSize = append(histBlockSize, &histBlockSizeItem{size: maxBlockPayload + 1})
	histTxSize = append(histTxSize, &histTxSizeItem{size: maxBlockPayload + 1})
	for f := uint32(5000); f < 3e8; f = uint32(float64(f) * 3) {
		histFeeRates = append(histFeeRates, &histFeeRateItem{feeRate: f})
	}
	histFeeRates = append(histFeeRates, &histFeeRateItem{feeRate: 3e8})
	for t := uint32(1); t < 5000; t = uint32(float64(t) * 2) {
		histTxCount = append(histTxCount, &histTxCountItem{txPerBlock: t})
	}

	var memPool, newTxs, minedTxs []*simTx

	estimator := newTxConfirmStats()

	// simulate a bunch of blocks.
	for h := uint32(1); h < lenSimulation; h++ {
		minedTxs, memPool = trivialMiner(memPool)
		newTxs = simGenTxs(h)
		// fmt.Printf("%d ; %d ; %d ; %d\n", h, len(minedTxs), len(newTxs), len(memPool))
		memPool = append(memPool, newTxs...)
		trackSimHistograms(minedTxs, newTxs)

		estimator.updateMovingAverages()
		for _, tx := range minedTxs {
			estimator.newMinedTx(int32(h-tx.genHeight), feeRate(tx.feeRate))
		}

		for _, tx := range newTxs {
			estimator.newMemPoolTx(feeRate(tx.feeRate))
		}

		// if h > 100 {
		// 	fmt.Println(estimator.dumpBuckets())

		// 	for _, t := range testMinConfs {
		// 		fee, err := estimator.estimateMedianFee(t, successPct)
		// 		if err != nil {
		// 			if err == ErrNoSuccessPctBucketFound {
		// 				fmt.Printf("noSuccBkt ")
		// 			} else if err == ErrNotEnoughTxsForEstimate {
		// 				fmt.Printf("notEnghTx ")
		// 			} else {
		// 				fmt.Printf("err       ")
		// 			}
		// 		} else {
		// 			fmt.Printf("%.5f ", fee/1e8)
		// 		}
		// 	}
		// 	fmt.Printf("\n")

		// 	break
		// }

		// fmt.Printf("%6.2f ", float64(totalTxsSizes(minedTxs))/1000.0)
		// if b%7 == 6 {
		// 	fmt.Println("")
		// }
	}

	// report the histogram of the simulated transactions to see if they are
	// reasonable
	reportSimHistograms()
}
