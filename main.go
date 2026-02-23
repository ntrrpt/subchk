package main

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/akamensky/argparse"
	"github.com/inhies/go-bytesize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/rs/zerolog"
)

var (
	log zerolog.Logger
	cfg *Config
)

type Config struct {
	/* global */
	input       string
	outputFile  string
	serveFile   string
	threadCount int

	/* results */
	resCount        int
	showFailed      bool
	showFailedTable bool

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
		Required: false,
		Help:     "url or file with proxies",
	})
	outputFile := parser.String("o", "output", &argparse.Options{
		Required: false,
		Help:     "write working proxies to file",
	})
	serveFile := parser.String("e", "server", &argparse.Options{
		Required: false,
		Help:     "run http server with output file content (:PORT or HOST:PORT)",
	})
	threadCount := parser.Int("t", "threads", &argparse.Options{
		Required: false,
		Help:     "number of threads",
		Default:  5,
	})

	/* results */
	resCount := parser.Int("r", "results", &argparse.Options{
		Required: false,
		Help:     "number of proxies to show in result table and write to output file (0 = print/write all)",
		Default:  0,
	})
	showFailed := parser.Flag("f", "failed", &argparse.Options{
		Required: false,
		Help:     "show dead proxies due testing",
	})
	showFailedTable := parser.Flag("", "ftable", &argparse.Options{
		Required: false,
		Help:     "show table with dead proxies",
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
		log.Fatal().Msg(parser.Usage(err))
	}

	return &Config{
		/* global */
		input:       *input,
		outputFile:  *outputFile,
		serveFile:   *serveFile,
		threadCount: *threadCount,
		/* results */
		resCount:        *resCount,
		showFailed:      *showFailed,
		showFailedTable: *showFailedTable,
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

func init() {
	log = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	cfg = parseArgs()
}

func main() {
	var sub string
	var err error

	if cfg.outputFile != "" && cfg.serveFile != "" {
		if serveFile(cfg.outputFile, cfg.serveFile) != nil {
			log.Panic().
				Err(err).
				Str("outputFile", cfg.outputFile).
				Str("addr", cfg.serveFile).
				Msg("failed to start server")
		}
		os.Exit(0)
	}

	if cfg.input == "" {
		log.Fatal().
			Msg("input file/url is empty (see --help)")
	}

	if _, err := bytesize.Parse(cfg.speedFilter); cfg.speedFilter != "" && err != nil {
		log.Panic().
			Err(err).
			Str("speed", cfg.speedFilter).
			Msg("failed to parse speedfilter")
	}

	if isFile(cfg.input) {
		sub, err = readFile(cfg.input)

		if err != nil {
			log.Panic().
				Err(err).
				Str("path", cfg.input).
				Msg("failed to read file")
		}
		log.Info().
			Str("input", cfg.input).
			Msg("file loaded")

	} else {
		_, err := url.Parse(cfg.input)
		if err != nil {
			log.Panic().
				Err(err).
				Str("url", cfg.input).
				Msg("invalid url")
		}

		sub, err = urlGet(cfg.input)
		if err != nil {
			log.Panic().
				Err(err).
				Str("url", cfg.input).
				Msg("failed to fetch sub")
		}

		log.Info().
			Str("input", cfg.input).
			Msg("url loaded")
	}

	urls := strings.Split(strings.ReplaceAll(sub, "\r\n", "\n"), "\n")
	log.Info().
		Int("jobs", len(urls)).
		Int("threads", cfg.threadCount).
		Msg("starting")

	results := runJobs(urls)

	var allResults []TestResult

	for res := range results {
		allResults = append(allResults, res)
	}

	// sort by ping
	slices.SortFunc(allResults, func(a, b TestResult) int {
		return int(a.Ping - b.Ping)
	})

	// sort by speed
	if cfg.speedTest && !cfg.pingSort {
		slices.SortFunc(allResults, func(a, b TestResult) int {
			switch {
			case a.Speed < b.Speed:
				return 1
			case a.Speed > b.Speed:
				return -1
			default:
				return 0
			}
		})
	}

	// show tables with good / bad proxies
	var outputUrls []string

	restab := table.NewWriter()
	restab.SetAutoIndex(true)
	restab.SetStyle(table.StyleColoredBright)
	resrow := table.Row{"id", "ip:port", "ping"}
	if cfg.speedTest {
		resrow = append(resrow, "speed", "time", "dwlen")
	}
	restab.AppendHeader(resrow)
	restab.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
	})

	errtab := table.NewWriter()
	errtab.SetAutoIndex(true)
	errtab.SetStyle(table.StyleColoredBlackOnRedWhite)
	errtab.AppendHeader(table.Row{"id", "ip:port", "error"})
	errtab.SetColumnConfigs([]table.ColumnConfig{
		{Number: 2, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
		{Number: 3, WidthMax: 100, WidthMaxEnforcer: text.WrapSoft},
	})

	for i, result := range allResults {
		if result.Error != nil {
			errtab.AppendRow(table.Row{
				result.ID,
				urlFix(result.Url),
				result.Error,
			})

			continue
		}

		if cfg.resCount > 0 && i >= cfg.resCount {
			continue
		}

		if result.Ping == 0 {
			continue
		}

		b, _ := bytesize.Parse(cfg.speedFilter)
		if b != 0 && result.Speed != 0 && b > result.Speed {
			continue
		}

		resInfo := table.Row{
			result.ID,
			urlFix(result.Url),
			result.Ping,
		}

		if cfg.speedTest {
			resInfo = append(resInfo,
				result.Speed.Format("%.2f ", "MB", false),
				fmt.Sprintf("%.2fs", result.Time),
				result.dwLen.Format("%.2f ", "MB", false))
		}

		restab.AppendRow(resInfo)

		outputUrls = append(outputUrls, result.Url)
	}

	if len(outputUrls) > 0 {
		log.Info().
			Msg("results:\n" + restab.Render())
	}

	if cfg.showFailedTable {
		log.Error().
			Msg("errors:\n" + errtab.Render())
	}

	// write output file
	if cfg.outputFile != "" {
		file, err := os.Create(cfg.outputFile)
		if err != nil {
			log.Panic().
				Err(err).
				Str("path", cfg.outputFile).
				Msg("failed to create output file")
		}
		defer file.Close()

		_, err = file.WriteString(strings.Join(outputUrls, "\n"))
		if err != nil {
			log.Panic().
				Err(err).
				Str("path", cfg.outputFile).
				Msg("failed to write to output file")
		}

		log.Info().
			Str("path", cfg.outputFile).
			Msg("writed output file")
	}

	os.Exit(0)
}
