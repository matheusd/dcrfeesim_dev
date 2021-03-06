# Estimatesmartfee

This repo is a Proof-of-Concept for the `estimatesmartfee` decred command. It's roughly based on Bitcoin core's [v0.14.2](https://github.com/bitcoin/bitcoin/blob/v0.14.2/src/policy/fees.cpp) code.

See the references below for some high level overviews of smart fee estimation in bitcoin.

This code runs a simulator (**not** decred's simnet) to check whether the algorithm is roughly correct.

## Simulator

The current simulator code is very simple: at every new block it generates a bunch of transactions following a distribution based on `rand.ExpFloat64()`, scaled to values eyeballed to look somewhat reasonable.

This probably needs serious improvements.

The miner is also very simple: it sorts txs by fee rate and includes txs until the block is filled. It doesn't use priority rules nor tries to fill the remaining space by using remaining transactions.

## Estimator

The basic idea of the estimator is to track how many transactions are mined at each fee rate bucket/confirmation rate bucket.

A fee rate bucket groups transactions that have fees within a given range (eg: transactions that are paying 0.0010-0.0015 DCR/KB as transaction fees).

A confirmation rate bucket tracks transactions confirmed within a given window after being seen on the mempool (eg: transactions included within 8-10 blocks after being published to the network). Right now this is tracked individually.

After seeing a number of transactions, the estimator can then estimate the median fee paid by transactions confirmed within X blocks after being published to the network by looking at the buckets at the desired confirmation level. It tries to minimize the fees by looking backwards (that is, starting at the highest fee bucket) until less than 95% of the transactions have been mined at the given confirmation/bucket level.

## Results

This is the important bit. What should I use as fee rate (in DCR/KB) if I want to have the tx confirmed in at most N blocks?

### Test Case 01

([Full results](results/testcase01.txt)). This is the base test for
other cases. Highlights of the parameters for this case:

- Generated transactions always pay a minimum fee rate of 0.0001 DCR/KB
- After mining a block the mempool still has transactions left ~59% of the time
  (so there's still quite some room left in blocks)
- Using a maximum of 32 confirmation windows
- Using a 1.1 fee bucket multiplier

```
=== Fees to use for minConf confirmations ===
           1           2           3           4           5           6           8          16          32
  0.00048193  0.00036204  0.00029931  0.00027212  0.00024740  0.00022499  0.00020446  0.00013970  0.00010000
```

### Test Case 02

([Full results](results/testcase02.txt)). Based on test 01, with the following
changes:

- Higher rate of generated transactions per block (> 99% of the blocks leave
  transactions in mempool after mining)

```
=== Fees to use for minConf confirmations ===
           1           2           4           6           8          12          18          24          32
  0.00052973  0.00043813  0.00032930  0.00029928  0.00024740  0.00020449  0.00020449  0.00018592  0.00012713
```

### Test Case 03

([Full results](results/testcase03.txt)). Based on test 01, with the following
changes:

- Transactions are not generated with minimum fees (so they have a higher
  distribution of fee rates)

```
=== Fees to use for minConf confirmations ===
           1           2           3           4           5           6           8          16          32
  0.00035126  0.00026400  0.00019842  0.00016405  0.00014902  0.00011200  0.00010183  0.00003243  0.00000050
```

### Test Case 04

([Full results](results/testcase04.txt)). Based on test 02 with the following changes:

- No minimum fee

```
=== Fees to use for minConf confirmations ===
           1           2           4           6           8          12          16          32
  0.00046736  0.00031933  0.00024006  0.00019848  0.00014911  0.00011201  0.00010181  0.00002438
```

### Test Case 05

([Full results](results/testcase05.txt)). Based on test 01 with the following changes:

- Lower contention (~5% of blocks mined leave txs in mempool).

```
=== Fees to use for minConf confirmations ===
           1           2           3           4           5           6           8          10          16
  0.00020457  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000
```

### Test Case 06

([Full results](results/testcase06.txt)). Based on test 05 with the following changes:

- Smaller simulated fee range distribution

```
=== Fees to use for minConf confirmations ===
           1           2           3           4           5           6           8          10          16
  0.00010100  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000
```

### Test Case 07

([Full results](results/testcase07.txt)). Based on test 01 with the following changes:

- Lower contention (~10% of blocks mined leave txs in mempool).


```
=== Fees to use for minConf confirmations ===
           1           2           4           6           8          16          24          32
  0.00024752  0.00012700  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000  0.00010000
```


## References

https://bitcointechtalk.com/an-introduction-to-bitcoin-core-fee-estimation-27920880ad0

https://gist.github.com/morcos/d3637f015bc4e607e1fd10d8351e9f41
