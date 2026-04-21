package main

import (
	"fmt"
	"net/url"
	"os"
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

	restab := table.NewWriter()
	restab.SetAutoIndex(true)
	restab.SetStyle(table.StyleColoredBright)
	resrow := table.Row{"rawUrl", "rawSpeed", "id", "url", "ping"}
	if cfg.speedTest {
		resrow = append(resrow, "speed", "time", "dwlen")
	}
	restab.AppendHeader(resrow)
	restab.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Hidden: true},
		{Number: 2, Hidden: true},
		{Number: 4, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
	})

	errtab := table.NewWriter()
	errtab.SetAutoIndex(true)
	errtab.SetStyle(table.StyleColoredBlackOnRedWhite)
	errtab.AppendHeader(table.Row{"id", "url", "error"})
	errtab.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, WidthMax: 50, WidthMaxEnforcer: text.WrapSoft},
		{Number: 2, WidthMax: 100, WidthMaxEnforcer: text.WrapSoft},
	})

	var dwLenTotal bytesize.ByteSize

	for _, result := range results {
		if result.Error != nil {
			errtab.AppendRow(table.Row{
				result.ID, urlFix(result.Url), result.Error,
			})

			continue
		}

		resInfo := table.Row{
			result.Url, result.Speed, result.ID, urlFix(result.Url), result.Ping,
		}

		if cfg.speedTest {
			resInfo = append(resInfo,
				result.Speed.Format("%.2f ", "MB", false),
				fmt.Sprintf("%.2fs", result.Time),
				result.dwLen.Format("%.2f ", "MB", false))
		}

		dwLenTotal += result.dwLen

		restab.AppendRow(resInfo)
	}

	// filtering by zero ping
	restab.FilterBy([]table.FilterBy{
		{Name: "ping", Operator: table.NotEqual, Value: 0},
	})

	// sort by ping
	restab.SortBy([]table.SortBy{
		{Name: "ping", Mode: table.AscNumeric},
	})

	// filtering by speed
	if cfg.speedTest && speedFilter != 0 {
		restab.FilterBy([]table.FilterBy{
			{
				Number: 2,
				CustomFilter: func(rawSpeed string) bool {
					b, err := bytesize.Parse(rawSpeed)
					if err == nil && b != 0 && b >= speedFilter {
						return true
					}
					return false
				},
			},
		})
	}

	// sort by speed
	if cfg.speedTest && !cfg.pingSort {
		restab.SortBy([]table.SortBy{
			{
				Number: 2,
				CustomLess: func(iStr string, jStr string) int {
					iNum, iErr := bytesize.Parse(iStr)
					jNum, jErr := bytesize.Parse(jStr)

					if iErr != nil || jErr != nil {
						// fallback to string comparison if not numeric
						if iStr < jStr {
							return 1
						}
						if iStr > jStr {
							return -1
						}
						return 0
					}

					if iNum < jNum {
						return 1
					}
					if iNum > jNum {
						return -1
					}
					return 0
				},
			},
		})

	}

	if cfg.trace && errtab.Length() > 0 {
		errtab.SortBy([]table.SortBy{
			{Number: 1, Mode: table.AscNumeric},
		})

		println(errtab.Render())
	}

	if restab.Length() > 0 {
		restabRender := restab.Render()
		if cfg.resCount > 0 {
			restab.SetPageSize(cfg.resCount)
			restabRender = strings.SplitN(restab.Render(), "\n\n", 2)[0]
		}
		println(restabRender)
	}

	log.Info().
		Str("dwLenTotal", dwLenTotal.Format("%.2f ", "MB", false)).
		Msg("finished")

	// clean style without colors and borders
	emptyStyle := table.Style{
		Name: "emptyStyle",
		Box: table.BoxStyle{
			BottomLeft:       "",
			BottomRight:      "",
			BottomSeparator:  "",
			EmptySeparator:   "",
			Left:             "",
			LeftSeparator:    "",
			MiddleHorizontal: "",
			MiddleSeparator:  "",
			MiddleVertical:   "",
			PaddingLeft:      "",
			PaddingRight:     "",
			PageSeparator:    "",
			Right:            "",
			RightSeparator:   "",
			TopLeft:          "",
			TopRight:         "",
			TopSeparator:     "",
			UnfinishedRow:    "",
		},
		Color:   table.ColorOptionsDefault,
		Format:  table.FormatOptionsDefault,
		HTML:    table.DefaultHTMLOptions,
		Options: table.OptionsNoBordersAndSeparators,
		Size:    table.SizeOptionsDefault,
		Title:   table.TitleOptionsDefault,
	}

	restab.SetAutoIndex(false)
	restab.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Hidden: false},
		{Number: 2, Hidden: true},
		{Number: 3, Hidden: true},
		{Number: 4, Hidden: true},
		{Number: 5, Hidden: true},
		{Number: 6, Hidden: true},
		{Number: 7, Hidden: true},
		{Number: 8, Hidden: true},
		{Number: 9, Hidden: true},
	})
	restab.SetStyle(emptyStyle)
	restabTxtRender := extractRawUrl(restab.Render())

	//write output file
	if cfg.outputFile != "" && len(restabTxtRender) > 0 {
		file, err := os.Create(cfg.outputFile)
		if err != nil {
			log.Panic().
				Err(err).
				Str("path", cfg.outputFile).
				Msg("failed to create output file")
		}
		defer file.Close()

		_, err = file.WriteString(restabTxtRender)
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
