package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/inhies/go-bytesize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/rs/zerolog"
)

var (
	log      zerolog.Logger
	cliFlags *flag.FlagSet
)

// cli flags
var (
	src string

	threadCount     int
	speedTest       bool
	resultCount     int
	sortByPing      bool
	showFailed      bool
	showFailedTable bool

	pingTimeout  int
	speedTimeout int

	pingUrl  string
	speedUrl string

	outputFile  string
	speedFilter string
)

func init() {
	log = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	cliFlags = flag.NewFlagSet("subchk", flag.ExitOnError)

	cliFlags.StringVar(&src, "sub", "", "path to subscription (file or url)")
	cliFlags.IntVar(&threadCount, "threads", 2, "number of threads")
	cliFlags.IntVar(&resultCount, "res", 0, "number of proxies to show (default: 0 = print/write all)")
	cliFlags.BoolVar(&sortByPing, "pingsort", false, "sorting proxies by ping, even if speedtest is enabled")
	cliFlags.StringVar(&outputFile, "output", "", "write working proxies to file")

	cliFlags.BoolVar(&showFailed, "failed", false, "show dead proxies due testing")
	cliFlags.BoolVar(&showFailedTable, "failedtable", false, "show table with dead proxies")

	cliFlags.StringVar(&pingUrl, "pingurl", "https://www.google.com/generate_204", "url to ping")
	cliFlags.IntVar(&pingTimeout, "pingtimeout", 3, "ping timeout")

	cliFlags.BoolVar(&speedTest, "speed", false, "enable speed test")
	cliFlags.StringVar(&speedUrl, "speedurl", "https://speed.cloudflare.com/__down?bytes=10000000", "url for speed test")
	cliFlags.IntVar(&speedTimeout, "speedtimeout", 10, "speed test timeout")
	cliFlags.StringVar(&speedFilter, "speedfilter", "", "filter proxies by speed (ex. 10MB, 4096kb)")
}

func main() {
	var sub string
	var err error

	cliFlags.Parse(os.Args[1:])

	if src == "" {
		cliFlags.Usage()
		os.Exit(1)
	}

	if _, err := bytesize.Parse(speedFilter); speedFilter != "" && err != nil {
		log.Panic().
			Err(err).
			Str("speed", speedFilter).
			Msg("failed to parse speedfilter")
	}

	if isFile(src) {
		sub, err = readFile(src)

		if err != nil {
			log.Panic().
				Err(err).
				Str("path", src).
				Msg("failed to read file")
		}
		log.Info().Str("src", src).Msg("file loaded")

	} else {
		_, err := url.Parse(src)
		if err != nil {
			log.Panic().
				Err(err).
				Str("url", src).
				Msg("invalid url")
		}

		sub, err = urlGet(src)
		if err != nil {
			log.Panic().
				Err(err).
				Str("url", src).
				Msg("failed to fetch sub")
		}

		log.Info().Str("src", src).Msg("url loaded")
	}

	urls := strings.Split(strings.ReplaceAll(sub, "\r\n", "\n"), "\n")
	log.Info().
		Int("jobs", len(urls)).
		Int("threads", threadCount).
		Msg("starting")

	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		sigCount := 0
		for range sigCh {
			sigCount++

			switch sigCount {
			case 1: // 1st ctrl-c -> cancel future workers
				log.Info().Msg("stop sending tasks")
				cancel()
			case 2: // 2nd ctrl-c -> 腹切り
				log.Fatal().Msg("force exit")
			}
		}
	}()

	jobs := make(chan TestJob)
	results := make(chan TestResult, len(urls))
	var wg sync.WaitGroup

	for w := 1; w <= threadCount; w++ {
		wg.Add(1)
		go worker(ctx, w, jobs, results, &wg)
	}

	go func() {
		defer close(jobs)
		for i, url := range urls {
			if url == "" {
				continue
			}

			select {
			case jobs <- TestJob{ID: i, URL: url}:
			case <-ctx.Done():
				log.Warn().Msg("stopped submitting jobs")
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	var allResults []TestResult

	// appendind all results
	for res := range results {
		allResults = append(allResults, res)
	}

	// sort by ping
	slices.SortFunc(allResults, func(a, b TestResult) int {
		return int(a.Ping - b.Ping)
	})

	// sort by speed
	if speedTest && !sortByPing {
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
	if speedTest {
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

		if resultCount > 0 && i >= resultCount {
			continue
		}

		if result.Ping == 0 {
			continue
		}

		b, _ := bytesize.Parse(speedFilter)
		if b != 0 && result.Speed != 0 && b > result.Speed {
			continue
		}

		resInfo := table.Row{
			result.ID,
			urlFix(result.Url),
			result.Ping,
		}

		if speedTest {
			resInfo = append(resInfo,
				result.Speed.Format("%.2f ", "MB", false),
				fmt.Sprintf("%.2fs", result.Time),
				result.dwLen.Format("%.2f ", "MB", false))
		}

		restab.AppendRow(resInfo)

		outputUrls = append(outputUrls, result.Url)
	}

	if len(outputUrls) > 0 {
		log.Info().Msg("results:\n" + restab.Render())
	}

	if showFailedTable {
		log.Error().Msg("errors:\n" + errtab.Render())
	}

	// write output file
	if outputFile != "" {
		file, err := os.Create(outputFile)
		if err != nil {
			log.Panic().
				Err(err).
				Str("path", outputFile).
				Msg("failed to create output file")
		}
		defer file.Close()

		_, err = file.WriteString(strings.Join(outputUrls, "\n"))
		if err != nil {
			log.Panic().
				Err(err).
				Str("path", outputFile).
				Msg("failed to write to output file")
		}

		log.Info().
			Str("path", outputFile).
			Msg("writed output file")
	}

	os.Exit(0)
}
