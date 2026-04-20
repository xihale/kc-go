package auth

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	loginTimeout    = 3 * time.Second
	loginRetryDelay = 500 * time.Millisecond
)

var loginClient = &http.Client{
	Timeout: loginTimeout,
}

func LoginPortal(user, pass, ip, portalBaseURL, acIP string) (string, error) {
	if user == "" || pass == "" {
		return "", fmt.Errorf("credentials not provided, skipping auth")
	}

	apiURL := fmt.Sprintf(
		"%s/eportal/portal/login?callback=dr1004&login_method=1&user_account=%%2C0%%2C%s&user_password=%s&wlan_user_ip=%s&wlan_user_ipv6=&wlan_ac_ip=%s&wlan_ac_name=&jsVersion=4.1.3&terminal_type=1&lang=zh-cn&v=6985&lang=zh",
		portalBaseURL, user, pass, ip, acIP,
	)
	return doLogin(apiURL)
}

func LoginPortalFromRedirect(user, pass, ip, redirectLocation, portalBaseURL, acIP string) (string, error) {
	if user == "" || pass == "" {
		return "", fmt.Errorf("credentials not provided, skipping auth")
	}

	portalBase := extractPortalBase(redirectLocation)
	if portalBase == "" {
		return LoginPortal(user, pass, ip, portalBaseURL, acIP)
	}

	apiURL := fmt.Sprintf(
		"%s?callback=dr1004&login_method=1&user_account=%%2C0%%2C%s&user_password=%s&wlan_user_ip=%s&wlan_user_ipv6=&wlan_ac_ip=%s&wlan_ac_name=&jsVersion=4.1.3&terminal_type=1&lang=zh-cn&v=6985&lang=zh",
		portalBase, user, pass, ip, acIP,
	)
	return doLogin(apiURL)
}

func doLogin(apiURL string) (string, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	req.Header.Set("Referer", "http://10.0.3.2/")

	resp, err := loginClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return string(body), fmt.Errorf("login failed with status %d", resp.StatusCode)
	}
	return string(body), nil
}

func IsLoginSuccess(body string) bool {
	return strings.Contains(body, "认证成功") || strings.Contains(body, "已经在线")
}

func extractPortalBase(redirectURL string) string {
	if redirectURL == "" {
		return ""
	}
	u, err := url.Parse(redirectURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return fmt.Sprintf("%s://%s/eportal/portal/login", u.Scheme, u.Host)
}

func LoginWithRetry(user, pass, ip, redirectLocation, portalBaseURL, acIP string, retries int) (string, error) {
	return LoginWithRetryDelay(user, pass, ip, redirectLocation, portalBaseURL, acIP, retries, loginRetryDelay)
}

func LoginWithRetryDelay(user, pass, ip, redirectLocation, portalBaseURL, acIP string, retries int, retryDelay time.Duration) (string, error) {
	if retries < 1 {
		retries = 1
	}
	if retryDelay < 0 {
		retryDelay = 0
	}

	var lastErr error
	var lastBody string
	for i := 0; i < retries; i++ {
		var body string
		var err error
		if redirectLocation != "" {
			body, err = LoginPortalFromRedirect(user, pass, ip, redirectLocation, portalBaseURL, acIP)
		} else {
			body, err = LoginPortal(user, pass, ip, portalBaseURL, acIP)
		}
		if err == nil {
			return body, nil
		}
		lastBody = body
		lastErr = err
		if i < retries-1 && retryDelay > 0 {
			time.Sleep(retryDelay)
		}
	}
	return lastBody, fmt.Errorf("login failed after %d attempts: %w", retries, lastErr)
}
