#!/usr/bin/env bash

# go through the directories and run the commands

for dir in $(ls -d */); do
  echo "Running tests in $dir"
  pushd $dir > /dev/null

  # run the command
  yamltrimmer

  # check if the expected.yaml file is equal to the output.yaml file

  if [ ! -f "expected.yaml" ]; then
    echo "expected.yaml does not exist"
    exit 1
  fi

  if [ ! -f "output.yaml" ]; then
    echo "output.yaml does not exist"
    exit 1
  fi

  if ! diff -q expected.yaml output.yaml; then
    echo "expected.yaml and output.yaml are not equal"
    exit 1
  fi

  popd > /dev/null
done
