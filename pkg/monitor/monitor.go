package monitor

import (
	"net/http"
	"time"
)

const probeTimeout = time.Second

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

func CheckConnectivity(url string) (CheckResult, int, string, error) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return ResultFailed, 0, "", err
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return ResultFailed, 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return ResultPortal, resp.StatusCode, resp.Header.Get("Location"), nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ResultSuccess, resp.StatusCode, "", nil
	}
	return ResultFailed, resp.StatusCode, "", nil
}
