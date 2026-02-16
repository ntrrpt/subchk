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

	outputFile string
)

func init() {
	log = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
	).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	cliFlags = flag.NewFlagSet("subchk", flag.ExitOnError)

	cliFlags.StringVar(&src, "i", "", "path to subscription\n(file or url)")
	cliFlags.IntVar(&threadCount, "t", 2, "number of threads")
	cliFlags.IntVar(&resultCount, "c", 0, "number of results to be processed\n(default: 0 = print/write all)")
	cliFlags.BoolVar(&sortByPing, "ps", false, "sorting results by ping, even if speedtest is enabled")
	cliFlags.StringVar(&outputFile, "o", "", "write result url's to file")

	cliFlags.BoolVar(&showFailed, "f", false, "show failed results due testing")
	cliFlags.BoolVar(&showFailedTable, "ft", false, "show table with failed results")

	cliFlags.StringVar(&pingUrl, "pu", "https://www.google.com/generate_204", "url to ping")
	cliFlags.IntVar(&pingTimeout, "pt", 3, "ping timeout")

	cliFlags.BoolVar(&speedTest, "s", false, "enable speed test")
	cliFlags.StringVar(&speedUrl, "su", "https://speed.cloudflare.com/__down?bytes=10000000", "url for speed test")
	cliFlags.IntVar(&speedTimeout, "st", 10, "speed test timeout")
}

func main() {
	var sub string
	var err error

	cliFlags.Parse(os.Args[1:])

	if src == "" {
		log.Fatal().Msg("empty src")
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

		resInfo := table.Row{
			result.ID,
			urlFix(result.Url),
			result.Ping,
		}

		if speedTest {
			resInfo = append(resInfo,
				fmt.Sprintf("%.2f MB/s", result.Speed),
				fmt.Sprintf("%.2fs", result.Time),
				fmt.Sprintf("%.2f MB", result.dwLen/1024/1024))
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
