#!/bin/sh

go build -o sim *.go

END=7
for i in $(seq -f "%02g" 1 $END); do
  echo "test $i"
  ./sim $i > "results/testcase$i.txt"
done
