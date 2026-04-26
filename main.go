package main

import (
	"fmt"
	"net/url"
	"os"
	"sort"
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

type okResult struct {
	ID    int
	URL   string
	Ping  int64
	Speed bytesize.ByteSize
	Time  float64
	dwLen bytesize.ByteSize
}

type ngResult struct { // not good
	ID    int
	URL   string
	Error error
}

func init() {
	cfg = parseArgs()

	if cfg.trace {

		log = zerolog.New(
			zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.DateTime},
		).Level(zerolog.TraceLevel).With().Timestamp().Caller().Logger()

	} else if cfg.debug {

		log = zerolog.New(
			zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
		).Level(zerolog.DebugLevel).With().Timestamp().Logger()

	} else {

		log = zerolog.New(
			zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.TimeOnly},
		).Level(zerolog.InfoLevel).With().Timestamp().Logger()

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

	speedFilter, err := bytesize.Parse(cfg.speedFilter)
	if cfg.speedFilter != "" && err != nil {
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

	results := make([]TestResult, 0)

	for res := range runJobs(urls) {
		results = append(results, res)
	}

	/*

	 */

	var dwLenTotal bytesize.ByteSize

	okResults := []okResult{}
	ngResults := []ngResult{}

	for _, result := range results {
		if result.Error != nil {

			ngResults = append(ngResults,
				ngResult{
					result.ID,
					urlFix(result.Url),
					result.Error,
				},
			)

		} else if result.Ping > 0 {

			okResults = append(okResults,
				okResult{
					result.ID,
					result.Url,
					result.Ping,
					result.Speed,
					result.Time,
					result.dwLen,
				},
			)

			dwLenTotal += result.dwLen
		}
	}

	sort.Slice(okResults, func(i, j int) bool {
		return okResults[i].Ping < okResults[j].Ping
	})

	// filtering by speed
	if cfg.speedTest && speedFilter > 0 {
		filtered := okResults[:0]

		for _, res := range okResults {
			if res.Speed >= speedFilter {
				filtered = append(filtered, res)
			}
		}

		okResults = filtered
	}

	// sort by speed
	if cfg.speedTest && !cfg.pingSort {
		sort.Slice(okResults, func(i, j int) bool {
			return okResults[i].Speed > okResults[j].Speed
		})
	}

	// err table
	if cfg.trace && len(ngResults) > 0 {
		sort.Slice(ngResults, func(i, j int) bool {
			return ngResults[i].ID < ngResults[j].ID
		})

		ngCols := []func(ngResult) any{
			func(r ngResult) any { return r.ID },
			func(r ngResult) any { return urlFix(r.URL) },
			func(r ngResult) any { return r.Error },
		}

		ngResultsAny := MapTable(ngResults, ngCols)

		ngtab := table.NewWriter()
		ngtab.SetAutoIndex(true)
		ngtab.SetStyle(table.StyleColoredBlackOnRedWhite)
		ngtab.AppendHeader(table.Row{"id", "url", "error"})
		ngtab.SetColumnConfigs([]table.ColumnConfig{
			{Number: 1, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
			{Number: 2, WidthMax: 100, WidthMaxEnforcer: text.WrapSoft},
		})
		ngtab.ImportGrid(ngResultsAny)

		println(ngtab.Render())
	}

	if len(okResults) > 0 {
		okCols := []func(okResult) any{
			func(r okResult) any { return r.ID },
			func(r okResult) any { return urlFix(r.URL) },
			func(r okResult) any { return r.Ping },
			func(r okResult) any { return r.Speed.Format("%.2f ", "MB", false) },
			func(r okResult) any { return fmt.Sprintf("%.2fs", r.Time) },
			func(r okResult) any { return r.dwLen.Format("%.2f ", "MB", false) },
		}

		okResultsAny := MapTable(okResults, okCols)

		if cfg.resCount > 0 && len(okResultsAny) > cfg.resCount {
			okResultsAny = okResultsAny[:cfg.resCount]
		}

		oktab := table.NewWriter()
		oktab.SetAutoIndex(true)
		oktab.SetStyle(table.StyleColoredBright)
		oktab.AppendHeader(table.Row{"id", "url", "ping", "speed", "time", "dwlen"})
		oktabCfg := []table.ColumnConfig{
			{Number: 2, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
		}
		if !cfg.speedTest {
			oktabCfg = []table.ColumnConfig{
				{Number: 2, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
				{Number: 4, Hidden: true},
				{Number: 5, Hidden: true},
				{Number: 6, Hidden: true},
			}
		}
		oktab.SetColumnConfigs(oktabCfg)

		oktab.ImportGrid(okResultsAny)

		println(oktab.Render())
	}

	log.Info().
		Int("ok", len(okResults)).
		Int("ng", len(ngResults)).
		Str("dwLenTotal", dwLenTotal.Format("%.2f ", "MB", false)).
		Msg("finished")

	// write output file
	if cfg.outputFile != "" && len(okResults) > 0 {
		var outputUrls []string

		for i, res := range okResults {
			if cfg.resCount == 0 || i < cfg.resCount {
				outputUrls = append(outputUrls, res.URL)
			}
		}

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
