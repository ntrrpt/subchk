package main

import (
	"os"

	"github.com/akamensky/argparse"
)

type Config struct {
	/* global */
	input       string
	outputFile  string
	serveFile   string
	threadCount int
	debug       bool
	trace       bool

	/* results */
	resCount int

	/* ping */
	pingSort    bool
	pingUrl     string
	pingTimeout int

	/* speed */
	speedTest    bool
	speedTimeout int
	speedUrl     string
	speedFilter  string
}

func parseArgs() *Config {
	parser := argparse.NewParser("subchk", "xray subs tester")

	/* global */
	input := parser.String("i", "input", &argparse.Options{
		Required: true,
		Help:     "url or file with proxies",
	})
	outputFile := parser.String("o", "output", &argparse.Options{
		Required: false,
		Help:     "write working proxies to file",
	})
	serveFile := parser.String("e", "server", &argparse.Options{
		Required: false,
		Help:     "serve http server with input file (PORT or HOST:PORT)",
	})
	threadCount := parser.Int("t", "threads", &argparse.Options{
		Required: false,
		Help:     "number of threads",
		Default:  5,
	})
	debug := parser.Flag("v", "debug", &argparse.Options{
		Required: false,
		Help:     "debug logging (show dead proxies due testing)",
	})
	trace := parser.Flag("", "trace", &argparse.Options{
		Required: false,
		Help:     "trace logging (show table with dead proxies)",
	})

	/* results */
	resCount := parser.Int("r", "results", &argparse.Options{
		Required: false,
		Help:     "number of proxies to show in result table and write to output file (0 = print/write all)",
		Default:  0,
	})

	/* ping */
	pingSort := parser.Flag("p", "pingsort", &argparse.Options{
		Required: false,
		Help:     "sorting proxies by ping, even if speedtest is enabled",
	})
	pingUrl := parser.String("", "purl", &argparse.Options{
		Required: false,
		Help:     "url to ping",
		Default:  "https://www.google.com/generate_204",
	})
	pingTimeout := parser.Int("", "ptimeout", &argparse.Options{
		Required: false,
		Help:     "ping timeout",
		Default:  3,
	})

	/* speed */
	speedTest := parser.Flag("s", "speed", &argparse.Options{
		Required: false,
		Help:     "enable speed test",
	})
	speedTimeout := parser.Int("", "stimeout", &argparse.Options{
		Required: false,
		Help:     "speed test timeout",
		Default:  10,
	})
	speedUrl := parser.String("", "surl", &argparse.Options{
		Required: false,
		Help:     "url for speed test",
		Default:  "https://speed.cloudflare.com/__down?bytes=10000000",
	})
	speedFilter := parser.String("", "sfilter", &argparse.Options{
		Required: false,
		Help:     "filter proxies by speed (ex. 10MB, 4096kb)",
	})

	if err := parser.Parse(os.Args); err != nil {
		println(parser.Usage(err))
		os.Exit(1)
	}

	return &Config{
		/* global */
		input:       *input,
		outputFile:  *outputFile,
		serveFile:   *serveFile,
		threadCount: *threadCount,
		debug:       *debug,
		trace:       *trace,

		/* results */
		resCount: *resCount,

		/* ping */
		pingSort:    *pingSort,
		pingUrl:     *pingUrl,
		pingTimeout: *pingTimeout,

		/* speed */
		speedTest:    *speedTest,
		speedTimeout: *speedTimeout,
		speedUrl:     *speedUrl,
		speedFilter:  *speedFilter,
	}
}
