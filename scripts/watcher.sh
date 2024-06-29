#!/bin/bash
reflex -r "\.go$" -s -- sh -c "go run . $*"
