package mjapi

import "testing"

// helper: build a CBOR text string (major 3) for short strings
func cborStr(s string) []byte {
	b := []byte{}
	if len(s) < 24 {
		b = append(b, 0x60|byte(len(s)))
	} else {
		b = append(b, 0x78, byte(len(s)))
	}
	return append(b, s...)
}

func TestCBORDecodeMap(t *testing.T) {
	// map(3): {"current_status":"running","percent":42,"job_id":"abc"}
	var b []byte
	b = append(b, 0xa3) // map of 3
	b = append(b, cborStr("current_status")...)
	b = append(b, cborStr("running")...)
	b = append(b, cborStr("percent")...)
	b = append(b, 0x18, 42) // uint8 42
	b = append(b, cborStr("job_id")...)
	b = append(b, cborStr("abc")...)

	v, err := CBORDecode(b)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not a map: %T", v)
	}
	if m["current_status"] != "running" {
		t.Errorf("status = %v", m["current_status"])
	}
}

func TestParseProgress(t *testing.T) {
	var b []byte
	b = append(b, 0xa3)
	b = append(b, cborStr("job_id")...)
	b = append(b, cborStr("f4af08e8")...)
	b = append(b, cborStr("current_status")...)
	b = append(b, cborStr("running")...)
	b = append(b, cborStr("percent")...)
	b = append(b, 0x18, 73) // 73%

	p, ok := ParseProgress(b)
	if !ok {
		t.Fatal("expected ok")
	}
	if p.Status != StatusRunning || p.Percent != 73 || p.JobID != "f4af08e8" {
		t.Errorf("progress = %+v", p)
	}
}

func TestParseProgressLiveKey(t *testing.T) {
	// real web frame: {"current_status":"running","percentage_complete":42}
	var b []byte
	b = append(b, 0xa2)
	b = append(b, cborStr("current_status")...)
	b = append(b, cborStr("running")...)
	b = append(b, cborStr("percentage_complete")...)
	b = append(b, 0x18, 42)
	p, ok := ParseProgress(b)
	if !ok || p.Percent != 42 || p.Status != StatusRunning {
		t.Errorf("live-key progress = %+v ok=%v", p, ok)
	}
}

func TestParseProgressFloatPercent(t *testing.T) {
	// percent as float64 (CBOR 0xfb)
	var b []byte
	b = append(b, 0xa2)
	b = append(b, cborStr("current_status")...)
	b = append(b, cborStr("running")...)
	b = append(b, cborStr("percent")...)
	b = append(b, 0xfb, 0x40, 0x49, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00) // 50.0
	p, ok := ParseProgress(b)
	if !ok || p.Percent != 50 {
		t.Errorf("float percent = %+v ok=%v", p, ok)
	}
}

func TestParseProgressHandshakeIgnored(t *testing.T) {
	// {"dtype":"user_success","user_id":"x"} has no current_status
	var b []byte
	b = append(b, 0xa2)
	b = append(b, cborStr("dtype")...)
	b = append(b, cborStr("user_success")...)
	b = append(b, cborStr("user_id")...)
	b = append(b, cborStr("x")...)
	if _, ok := ParseProgress(b); ok {
		t.Error("handshake frame should not parse as progress")
	}
}
