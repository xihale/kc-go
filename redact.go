package main

import (
	"regexp"
)

// sensitiveRe 匹配日志里可能出现的凭据片段，形式为 URL 查询参数：
//   user_password=xxx, token=xxx, password=xxx, secret=xxx, api_key=xxx
//
// 值内可含 URL 编码字符（如 %23）以及 "="，遇到 "&"、空白、引号即结束。
// 登录错误信息里 err.Error() 是完整的 login URL，内嵌明文密码，正是要处理的场景。
var sensitiveRe = regexp.MustCompile(
	`(?i)(user_password|password|token|secret|api[_-]?key)=([^&\s"']+)`,
)

// redact 把字符串里疑似密钥的值替换为 ***，避免凭据落入日志文件。
func redact(s string) string {
	if s == "" {
		return s
	}
	return sensitiveRe.ReplaceAllString(s, "${1}=***")
}
