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
	req, err := http.NewRequest("GET", listURL, nil)
	if err != nil {
		return false, fmt.Errorf("create list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := apiClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var listResp cfListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return false, fmt.Errorf("decode list response: %w", err)
	}
	if !listResp.Success || len(listResp.Result) == 0 {
		return false, fmt.Errorf("record not found for %s", name)
	}

	record := listResp.Result[0]
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
	req, err = http.NewRequest("PUT", updateURL, bytes.NewBuffer(payload))
	if err != nil {
		return false, fmt.Errorf("create update request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = apiClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read update response: %w", err)
	}
	if resp.StatusCode != 200 {
		return false, fmt.Errorf("update failed: %s", string(body))
	}

	var updateResp cfUpdateResponse
	if err := json.Unmarshal(body, &updateResp); err != nil {
		return false, fmt.Errorf("decode update response: %w", err)
	}
	if !updateResp.Success {
		msgs := make([]string, 0, len(updateResp.Errors))
		for _, e := range updateResp.Errors {
			msgs = append(msgs, e.Message)
		}
		return false, fmt.Errorf("update failed: %v", msgs)
	}

	return true, nil
}
