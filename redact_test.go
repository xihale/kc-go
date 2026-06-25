package main

import "testing"

func TestRedact(t *testing.T) {
	cases := map[string]string{
		// 远程日志里实际出现的形态：URL 内嵌明文密码（%23 是 URL 编码的 #）
		`http://10.0.3.2/eportal/portal/login?user_account=%2C0%2C20240001&user_password=secret%231234&wlan_user_ip=10.44.19.186`:
			`http://10.0.3.2/eportal/portal/login?user_account=%2C0%2C20240001&user_password=***&wlan_user_ip=10.44.19.186`,
		`token=abc123secret`:   `token=***`,
		`password=hunter2&x=1`: `password=***&x=1`,
		`api_key=k_12345`:      `api_key=***`,
		`nothing to see here`:  `nothing to see here`,
		``:                     ``,
	}
	for in, want := range cases {
		if got := redact(in); got != want {
			t.Errorf("redact(%q)\n  got  %q\n  want %q", in, got, want)
		}
	}
}

func TestRedactPreservesNonSensitive(t *testing.T) {
	// 登录成功的响应体（dr1004(...已经在线...)）不含敏感字段，不应被改动
	body := `dr1004({"result":0,"msg":"IP: 10.44.19.186 已经在线！","ret_code":2});`
	if got := redact(body); got != body {
		t.Errorf("redact changed a benign body: %q", got)
	}
}
