#!/bin/bash

CGO_ENABLED=0 GOOS=linux go build -mod=vendor -ldflags "-s -w" -o bin/ctx main.go
