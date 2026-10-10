#!/usr/bin/env bash

if [ "$DYNOTYPE" == "run" ]; then
  DISABLE_DATADOG_AGENT="true"
fi
