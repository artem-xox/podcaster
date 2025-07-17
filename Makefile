# Simple Makefile for Podcaster

.PHONY: build run

build:
	go build -o podcaster ./cmd/podcaster

run: build
	./podcaster
