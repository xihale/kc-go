package monitor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	probeTimeout   = 1200 * time.Millisecond
	probeBodyLimit = 512
)

var fallbackProbeURLs = []string{
	"http://connectivitycheck.gstatic.com/generate_204",
	"http://cp.cloudflare.com/generate_204",
}

var defaultClient = &http.Client{
	Timeout: probeTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type CheckResult int

const (
	ResultSuccess CheckResult = iota
	ResultPortal
	ResultFailed
)

type probeTarget struct {
	method string
	url    string
}

type probeOutcome struct {
	result   CheckResult
	code     int
	redirect string
	err      error
}

func CheckConnectivity(url string) (CheckResult, int, string, error) {
	targets := buildProbeTargets(url)
	results := make(chan probeOutcome, len(targets))

	for _, target := range targets {
		go func(target probeTarget) {
			results <- runProbe(target)
		}(target)
	}

	var portal probeOutcome
	var failed probeOutcome
	for i := 0; i < len(targets); i++ {
		outcome := <-results
		switch outcome.result {
		case ResultSuccess:
			return outcome.result, outcome.code, outcome.redirect, nil
		case ResultPortal:
			if portal.code == 0 {
				portal = outcome
			}
		case ResultFailed:
			if failed.code == 0 && failed.err == nil {
				failed = outcome
			}
		}
	}

	if portal.code != 0 || portal.redirect != "" {
		return portal.result, portal.code, portal.redirect, nil
	}
	return ResultFailed, failed.code, "", failed.err
}

func buildProbeTargets(primaryURL string) []probeTarget {
	targets := []probeTarget{
		{method: http.MethodHead, url: primaryURL},
		{method: http.MethodGet, url: primaryURL},
	}
	for _, fallbackURL := range fallbackProbeURLs {
		if fallbackURL == primaryURL {
			continue
		}
		targets = append(targets, probeTarget{method: http.MethodHead, url: fallbackURL})
	}
	return targets
}

func runProbe(target probeTarget) probeOutcome {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, target.method, target.url, nil)
	if err != nil {
		return probeOutcome{result: ResultFailed, err: err}
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return probeOutcome{result: ResultFailed, err: err}
	}
	defer resp.Body.Close()

	return classifyResponse(target, resp)
}

func classifyResponse(target probeTarget, resp *http.Response) probeOutcome {
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return probeOutcome{result: ResultPortal, code: resp.StatusCode, redirect: resp.Header.Get("Location")}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return probeOutcome{result: ResultFailed, code: resp.StatusCode}
	}

	if expects204(target.url) {
		if resp.StatusCode == http.StatusNoContent {
			return probeOutcome{result: ResultSuccess, code: resp.StatusCode}
		}
		if looksLikePortal(resp) {
			return probeOutcome{result: ResultPortal, code: resp.StatusCode, redirect: resp.Request.URL.String()}
		}
		return probeOutcome{result: ResultFailed, code: resp.StatusCode}
	}

	if target.method == http.MethodGet && looksLikePortal(resp) {
		return probeOutcome{result: ResultPortal, code: resp.StatusCode, redirect: resp.Request.URL.String()}
	}

	return probeOutcome{result: ResultSuccess, code: resp.StatusCode}
}

func expects204(url string) bool {
	return strings.Contains(url, "generate_204")
}

func looksLikePortal(resp *http.Response) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") {
		return true
	}
	if resp.ContentLength > 0 {
		return true
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
	if err != nil {
		return false
	}
	if len(body) == 0 {
		return false
	}

	content := strings.ToLower(string(body))
	return strings.Contains(content, "<html") || strings.Contains(content, "portal") || strings.Contains(content, "login")
}
