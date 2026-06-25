package main

import (
	"os"
	"strings"
	"time"
)

// applyTimezone 把本地时区应用到进程，使 log.LstdFlags 打本地时间而非 UTC。
//
// 优先级：配置 service.timezone > 已有 TZ 环境变量 > /etc/TZ（OpenWrt/BusyBox 约定）。
//
// OpenWrt 通常不带 zoneinfo（/usr/share/zoneinfo），Go 无法走标准 /etc/localtime，
// 因此这里用 time.FixedZone 解析 POSIX TZ 形如 "CST-8"——这是 BusyBox /etc/TZ 的格式。
// 单纯 os.Setenv("TZ") 不够：log 包格式化时读 time.Local，而 time.Local 只在首次访问
// 或 LoadLocation 时刷新，所以必须显式赋值 time.Local 才能让标准 log 打本地时间。
//
// 返回最终生效的时区字符串（空表示未设置、保持 Go 默认）。
func applyTimezone(configured string) string {
	tz := strings.TrimSpace(configured)
	if tz == "" {
		tz = strings.TrimSpace(os.Getenv("TZ"))
	}
	if tz == "" {
		if data, err := os.ReadFile("/etc/TZ"); err == nil {
			tz = strings.TrimSpace(string(data))
		}
	}
	if tz != "" {
		_ = os.Setenv("TZ", tz)
		if loc := parsePOSIXTZ(tz); loc != nil {
			time.Local = loc
		}
	}
	return tz
}

// parsePOSIXTZ 解析 POSIX TZ 字符串，如 "CST-8" / "UTC+0" / "IST-5:30"。
// 这是 OpenWrt /etc/TZ 的常见格式。POSIX 规则里符号是「反」的：偏移表示
// 「本地时间加上多少得到 UTC」，所以 "CST-8" 表示本地比 UTC 快 8 小时（东八区），
// "PST+8" 表示本地比 UTC 慢 8 小时（西八区）。解析失败返回 nil。
func parsePOSIXTZ(tz string) *time.Location {
	idx := -1
	for i := 0; i < len(tz); i++ {
		c := tz[i]
		if c == '+' || c == '-' {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return nil
	}
	name := tz[:idx]
	// POSIX 符号取反：'-' → 东区（正偏移），'+' → 西区（负偏移）
	sign := 1
	if tz[idx] == '+' {
		sign = -1
	}
	num := tz[idx+1:]
	h, m := 0, 0
	if colon := strings.IndexByte(num, ':'); colon >= 0 {
		h = atoi(num[:colon])
		m = atoi(num[colon+1:])
	} else {
		h = atoi(num)
	}
	return time.FixedZone(name, sign*(h*3600+m*60))
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}
