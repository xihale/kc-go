package main

import (
	"testing"
	"time"
)

func TestParsePOSIXTZ(t *testing.T) {
	cases := map[string]int{ // tz -> 本地相对 UTC 的偏移秒
		"CST-8":    8 * 3600, // OpenWrt /etc/TZ，东八区
		"UTC+0":    0,
		"IST-5:30": 5*3600 + 30*60,
		"PST+8":    -8 * 3600, // POSIX 符号相反：PST+8 = UTC-8
	}
	for tz, wantOff := range cases {
		loc := parsePOSIXTZ(tz)
		if loc == nil {
			t.Fatalf("parsePOSIXTZ(%q) = nil", tz)
		}
		// FixedZone 恒定偏移，任意时刻取 zone 都一样
		_, off := time.Date(2026, 1, 1, 0, 0, 0, 0, loc).Zone()
		if off != wantOff {
			t.Errorf("parsePOSIXTZ(%q) offset=%d want %d", tz, off, wantOff)
		}
	}
	if parsePOSIXTZ("garbage") != nil {
		t.Error("expected nil for malformed tz without sign")
	}
}
