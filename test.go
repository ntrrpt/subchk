package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
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

func worker(ctx context.Context, id int, jobs <-chan TestJob, results chan<- TestResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			log.Warn().Msgf("worker %d shutting down...", id)
			return

		case job, ok := <-jobs:
			if !ok {
				return
			}

			result := runTest(job)
			address := fmt.Sprintf("[%d] %-30s", job.ID, urlFix(job.URL))

			if result.Error == nil {
				l := log.Info().
					Int64("ping", result.Ping)

				if result.Speed > 0 {
					l = l.
						Str("transfered", result.dwLen.Format("%.2f ", "MB", false)).
						Str("speed", result.Speed.Format("%.2f ", "MB", false)).
						Str("duration", fmt.Sprintf("%.2fs", result.Time))
				}

				l.Msg(address)

			} else if showFailed {
				log.Error().
					Err(result.Error).
					Msg(address)
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

	client, err := proxyclient.New(job.URL, WithClientTimeout(time.Duration(pingTimeout)*time.Second))
	if err != nil {
		result.Error = err
		return result
	}

	// ping
	startPing := time.Now()
	presp, err := client.Get(pingUrl)
	if err != nil {
		result.Error = err
		return result
	}
	defer presp.Body.Close()

	result.Ping = time.Since(startPing).Milliseconds()

	if speedTest {
		// downloading test file
		client.Timeout = time.Duration(speedTimeout) * time.Second
		sresp, err := client.Get(speedUrl)

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
