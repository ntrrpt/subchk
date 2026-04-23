package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cnlangzi/proxyclient"
	"github.com/inhies/go-bytesize"

	_ "github.com/cnlangzi/proxyclient/xray"
)

type TestJob struct {
	ID  int
	URL string
}

type TestResult struct {
	ID    int
	Error error
	Url   string
	Ping  int64   // milliseconds
	Time  float64 // seconds
	Speed bytesize.ByteSize
	dwLen bytesize.ByteSize
}

func runJobs(urls []string) chan TestResult {
	ctx, cancel := context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		sigCount := 0

		for range sigCh {
			sigCount++

			switch sigCount {
			case 1: // 1st ctrl-c -> cancel workers
				log.Info().Msg("stopped sending tasks")
				cancel()
			case 2: // 2nd ctrl-c -> 腹切り
				log.Fatal().Msg("force exit")
			}
		}
	}()

	jobs := make(chan TestJob)
	results := make(chan TestResult, len(urls))
	var wg sync.WaitGroup

	for w := 1; w <= cfg.threadCount; w++ {
		wg.Add(1)
		go worker(ctx, w, jobs, results, &wg)
	}

	go func() {
		defer close(jobs)
		for i, u := range urls {
			if u == "" {
				log.Trace().
					Int("id", i).
					Msg("empty url")
				continue
			}

			if _, err := url.Parse(u); err != nil {
				log.Trace().
					Int("id", i).
					Str("url", u).
					Err(err).
					Msg("invalid url")
				continue
			}

			select {
			case jobs <- TestJob{ID: i, URL: u}:
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

	return results
}

func worker(ctx context.Context, id int, jobs <-chan TestJob, results chan<- TestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Warn().
				Msgf("worker %d shutting down...", id)
			return

		case job, ok := <-jobs:
			if !ok {
				return
			}

			result := runTest(job)
			address := fmt.Sprintf("%-30s", urlFix(job.URL))

			if result.Error == nil {
				l := log.Info().
					Int("id", result.ID).
					Int64("ping", result.Ping)

				if result.Speed > 0 {
					l = l.
						Str("transfered", result.dwLen.Format("%.2f ", "MB", false)).
						Str("speed", result.Speed.Format("%.2f ", "MB", false)).
						Str("duration", fmt.Sprintf("%.2fs", result.Time))
				}

				if cfg.trace {
					l = l.Str("url", job.URL)
				}

				l.Msg(address)

			} else if cfg.debug || cfg.trace {
				l := log.Error().
					Int("id", result.ID).
					Err(result.Error)

				if cfg.trace {
					l = l.Str("url", job.URL)
				}

				l.Msg(address)
			}

			select {
			case results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func WithClientTimeout(duration time.Duration) proxyclient.Option {
	client := &http.Client{
		Timeout: duration,
	}

	return func(o *proxyclient.Options) {
		o.Client = client
		o.Timeout = duration
	}
}

func runTest(job TestJob) TestResult {
	result := TestResult{
		ID:    job.ID,
		Url:   job.URL,
		Ping:  0,
		Speed: 0,
		Time:  0,
		dwLen: 0,
		Error: nil,
	}

	if _, err := url.Parse(job.URL); err != nil {
		result.Error = err
		return result
	}

	dur := time.Duration(cfg.pingTimeout) * time.Second
	client, err := proxyclient.New(job.URL, WithClientTimeout(dur))
	if err != nil {
		result.Error = err
		return result
	}

	// ping
	startPing := time.Now()
	presp, err := client.Get(cfg.pingUrl)
	if err != nil {
		result.Error = err
		return result
	}
	defer presp.Body.Close()

	result.Ping = time.Since(startPing).Milliseconds()

	if cfg.speedTest {
		// downloading test file
		client.Timeout = time.Duration(cfg.speedTimeout) * time.Second
		sresp, err := client.Get(cfg.speedUrl)

		if err != nil {
			result.Error = err
			return result
		}
		defer sresp.Body.Close()

		startDownload := time.Now()
		n, err := io.Copy(io.Discard, sresp.Body)

		result.dwLen = bytesize.New(float64(n))

		if err != nil && result.dwLen == 0 {
			result.Error = err
			return result
		}

		// measure download speed
		result.Time = time.Since(startDownload).Seconds()
		if result.Time == 0 {
			result.Error = fmt.Errorf("empty time, dwLen=%s", result.dwLen.Format("%.0f", "b", false))
			return result
		}

		result.Speed = bytesize.New(float64(n) / result.Time)
	}

	return result

}
