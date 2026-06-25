package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var apiClient = &http.Client{
	Timeout: 15 * time.Second,
}

const cfRetryDelay = 2 * time.Second

type cfListResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"result"`
}

type cfUpdateResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func UpdateRecord(token, zoneID, name, recordType, ip string) (bool, error) {
	listURL := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/zones/%s/dns_records?name=%s&type=%s",
		zoneID, name, recordType,
	)

	listResp, err := cfRequest(http.MethodGet, listURL, token, nil)
	if err != nil {
		return false, fmt.Errorf("list records: %w", err)
	}
	if listResp.StatusCode == http.StatusUnauthorized {
		return false, fmt.Errorf("cloudflare token rejected (401) — check API token")
	}
	if listResp.StatusCode < 200 || listResp.StatusCode >= 300 {
		return false, fmt.Errorf("list records failed with status %d", listResp.StatusCode)
	}

	var listResult cfListResponse
	if err := json.NewDecoder(listResp.Body).Decode(&listResult); err != nil {
		return false, fmt.Errorf("decode list response: %w", err)
	}
	if !listResult.Success || len(listResult.Result) == 0 {
		return false, fmt.Errorf("record not found for %s", name)
	}

	record := listResult.Result[0]
	if record.Content == ip {
		return false, nil
	}

	updateURL := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s",
		zoneID, record.ID,
	)
	payload, _ := json.Marshal(map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": ip,
		"ttl":     120,
	})

	updateResp, err := cfRequest(http.MethodPut, updateURL, token, payload)
	if err != nil {
		return false, fmt.Errorf("update record: %w", err)
	}
	defer updateResp.Body.Close()

	body, err := io.ReadAll(updateResp.Body)
	if err != nil {
		return false, fmt.Errorf("read update response: %w", err)
	}
	if updateResp.StatusCode == http.StatusUnauthorized {
		return false, fmt.Errorf("cloudflare token rejected (401) — check API token")
	}
	if updateResp.StatusCode < 200 || updateResp.StatusCode >= 300 {
		return false, fmt.Errorf("update failed with status %d: %s", updateResp.StatusCode, string(body))
	}

	var updateResult cfUpdateResponse
	if err := json.Unmarshal(body, &updateResult); err != nil {
		return false, fmt.Errorf("decode update response: %w", err)
	}
	if !updateResult.Success {
		msgs := make([]string, 0, len(updateResult.Errors))
		for _, e := range updateResult.Errors {
			msgs = append(msgs, e.Message)
		}
		return false, fmt.Errorf("update failed: %v", msgs)
	}

	return true, nil
}

// cfRequest 执行一次 Cloudflare API 调用。
// 对网络错误和 5xx 重试一次；4xx（含 401）立即返回由调用方处理。
func cfRequest(method, url, token string, payload []byte) (*http.Response, error) {
	do := func() (*http.Response, error) {
		var body io.Reader
		if payload != nil {
			body = bytes.NewBuffer(payload)
		}
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := apiClient.Do(req)
		if err != nil {
			return nil, err
		}
		// 5xx 视为可重试：先排空再关闭，复用底层连接
		if resp.StatusCode >= 500 {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("server error %d", resp.StatusCode)
		}
		return resp, nil
	}

	resp, err := do()
	if err == nil {
		return resp, nil
	}

	time.Sleep(cfRetryDelay)
	resp, err = do()
	if err != nil {
		return nil, fmt.Errorf("after retry: %w", err)
	}
	return resp, nil
}
