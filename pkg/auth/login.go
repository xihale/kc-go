package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	loginTimeout    = 3 * time.Second
	loginRetryDelay = 500 * time.Millisecond
)

var loginClient = &http.Client{
	Timeout: loginTimeout,
}

func LoginPortal(user, pass, ip, portalBaseURL, acIP string) error {
	if user == "" || pass == "" {
		return fmt.Errorf("credentials not provided, skipping auth")
	}

	apiURL := fmt.Sprintf(
		"%s/eportal/portal/login?callback=dr1004&login_method=1&user_account=%%2C0%%2C%s&user_password=%s&wlan_user_ip=%s&wlan_user_ipv6=&wlan_ac_ip=%s&wlan_ac_name=&jsVersion=4.1.3&terminal_type=1&lang=zh-cn&v=6985&lang=zh",
		portalBaseURL, user, pass, ip, acIP,
	)
	return doLogin(apiURL)
}

func LoginPortalFromRedirect(user, pass, ip, redirectLocation, portalBaseURL, acIP string) error {
	if user == "" || pass == "" {
		return fmt.Errorf("credentials not provided, skipping auth")
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

func doLogin(apiURL string) error {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	req.Header.Set("Referer", "http://10.0.3.2/")

	resp, err := loginClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("login failed with status %d", resp.StatusCode)
	}
	return nil
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

func LoginWithRetry(user, pass, ip, redirectLocation, portalBaseURL, acIP string, retries int) error {
	var lastErr error
	for i := 0; i < retries; i++ {
		var err error
		if redirectLocation != "" {
			err = LoginPortalFromRedirect(user, pass, ip, redirectLocation, portalBaseURL, acIP)
		} else {
			err = LoginPortal(user, pass, ip, portalBaseURL, acIP)
		}
		if err == nil {
			return nil
		}
		lastErr = err
		if i < retries-1 {
			time.Sleep(loginRetryDelay)
		}
	}
	return fmt.Errorf("login failed after %d attempts: %w", retries, lastErr)
}
