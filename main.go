package main

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/inhies/go-bytesize"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/rs/zerolog"
)

var (
	log zerolog.Logger
	cfg *Config
)

func init() {
	log = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
	).Level(zerolog.InfoLevel).With().Timestamp().Logger()

	cfg = parseArgs()

	if cfg.verbose {
		log = zerolog.New(
			zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
		).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()
	}
}

func main() {
	var sub string
	var err error

	if cfg.outputFile != "" && cfg.serveFile != "" {
		err = serveFile(cfg.outputFile, cfg.serveFile)
		if err != nil {
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
			Msg("input file/url is empty (see -h, --help)")
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
	resrow := table.Row{"id", "url", "ping"}
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
	errtab.AppendHeader(table.Row{"id", "url", "error"})
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

	if cfg.verbose {
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
