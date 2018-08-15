package main

import (
	"fmt"
	"os"
)

type testCase struct {
	simCfg       simulatorConfig
	estCfg       estimatorConfig
	testMinConfs []int32
}

var (
	sim *simulator

	// test scenario where blocks still aren't that filled and all transactions
	// are published with a minimum fee rate of 0.0001 dcr/KB
	testCase01 = testCase{
		simCfg: simulatorConfig{
			nbTxsCoef:      250.0,
			txSizeCoef:     1000.0,
			minimumFeeRate: 1e4,
			feeRateCoef:    2.5e4,
		},
		estCfg: estimatorConfig{
			maxConfirms:  8,
			minBucketFee: 9000,
			maxBucketFee: 4e5,
			feeRateStep:  1.1,
		},
		testMinConfs: []int32{1, 2, 3, 4, 5, 6, 8},
	}

	// test scenario where mempool is filled 99% of the time
	testCase02 = testCase{
		simCfg: simulatorConfig{
			nbTxsCoef:      320.0,
			txSizeCoef:     1000.0,
			minimumFeeRate: 1e4,
			feeRateCoef:    2.5e4,
		},
		estCfg: estimatorConfig{
			maxConfirms:  32,
			minBucketFee: 9000,
			maxBucketFee: 4e5,
			feeRateStep:  1.1,
		},
		testMinConfs: []int32{1, 2, 4, 6, 8, 12, 18, 24, 32},
	}

	// test scenario where there are no minimum relay fees
	testCase03 = testCase{
		simCfg: simulatorConfig{
			nbTxsCoef:      250.0,
			txSizeCoef:     1000.0,
			minimumFeeRate: 0,
			feeRateCoef:    2.5e4,
		},
		estCfg: estimatorConfig{
			maxConfirms:  8,
			minBucketFee: 100,
			maxBucketFee: 4e5,
			feeRateStep:  1.1,
		},
		testMinConfs: []int32{1, 2, 3, 4, 5, 6, 8},
	}

	// test scenario where there are no minimum relay fees and transactions are
	// generated at a higher rate and using a higher confirmation window
	testCase04 = testCase{
		simCfg: simulatorConfig{
			nbTxsCoef:      320.0,
			txSizeCoef:     1000.0,
			minimumFeeRate: 0,
			feeRateCoef:    2.5e4,
		},
		estCfg: estimatorConfig{
			maxConfirms:  16,
			minBucketFee: 100,
			maxBucketFee: 4e5,
			feeRateStep:  1.1,
		},
		testMinConfs: []int32{1, 2, 4, 6, 8, 12, 16},
	}

	// which of the scenarios to test
	actualTest = testCase02
)

func main() {
	// How long to run the simulation of blocks before trying to estimate the
	// fees
	lenSimulation := uint32(288 * 30 * 3)

	sim := newSimulator(&actualTest.simCfg)
	var memPool, newTxs, minedTxs []*simTx
	estimator := newTxConfirmStats(&actualTest.estCfg)

	// simulate a bunch of blocks. At every iteration, this simulates:
	// - a miner generating a new block from the current memPool
	// - some new transactions appearing in the network and being added to the
	// outstanding mempool
	for h := uint32(1); h < lenSimulation; h++ {
		minedTxs, memPool = sim.mineTransactions(h, memPool)
		newTxs = sim.genTransactions(h)
		memPool = append(memPool, newTxs...)
		sim.trackHistograms(minedTxs, newTxs)

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

		if h%(lenSimulation/100) == 0 {
			fmt.Fprintf(os.Stderr, "%d%% ", h*100/lenSimulation)
		}
	}
	fmt.Fprintf(os.Stderr, "\n\n")

	// Simulation has ended (eg: full node has synced)
	// Let's now try to estimate the fees.

	fmt.Println("=== Test Case Setup ===")
	fmt.Printf("%+v\n\n", actualTest)

	// Let's try generating fee rate estimates for a number of different minConf
	// amounts at the same success pct (this is roughly what bitcoin core does)
	fmt.Println("=== Fees to use for minConf confirmations ===")
	successPct := 0.95
	l1 := ""
	l2 := ""
	for _, t := range actualTest.testMinConfs {
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

	// report the histogram of the simulated transactions to see if they are
	// reasonable
	fmt.Println("=== Histograms for simulated data ===")
	sim.reportSimHistograms()

	// Let's see the internal state of the estimator
	fmt.Println("=== Internal Estimator State ===")
	fmt.Println(estimator.dumpBuckets())
}
