package mjapi

import "testing"

// Malformed/hostile CBOR frames must never panic the decoder (they reach
// ParseProgress on untrusted WebSocket bytes during `mj watch`). Each of these
// previously could overflow int(n) and trip a make()/slice-bounds panic.
func TestCBORHostileFramesNoPanic(t *testing.T) {
	cases := map[string][]byte{
		"bytestring len 2^64-1": {0x5b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"textstring len 2^64-1": {0x7b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"array count 2^64-1":    {0x9b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"map count 2^64-1":      {0xbb, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"bytestring 4GB len":    {0x5a, 0xff, 0xff, 0xff, 0xff},
		"truncated bytestring":  {0x43, 0x41}, // declares 3 bytes, has 1
		"truncated array":       {0x82, 0x01}, // declares 2 items, has 1
		"map odd/truncated":     {0xa1, 0x61}, // map(1) key text(1) but no key byte
		"empty":                 {},
		"tag then eof":          {0xc0},
	}
	for name, frame := range cases {
		// CBORDecode must return an error (or value) but never panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: CBORDecode panicked: %v", name, r)
				}
			}()
			_, _ = CBORDecode(frame)
		}()
		// ParseProgress must report ok=false and never panic.
		if _, ok := ParseProgress(frame); ok {
			t.Errorf("%s: ParseProgress returned ok=true for a malformed frame", name)
		}
	}
}

// A well-formed progress map still decodes correctly after the hardening.
func TestCBORValidProgressStillWorks(t *testing.T) {
	// map(2){ "current_status": "running", "percentage_complete": 42 }
	frame := []byte{
		0xa2,
		0x6e, 'c', 'u', 'r', 'r', 'e', 'n', 't', '_', 's', 't', 'a', 't', 'u', 's',
		0x67, 'r', 'u', 'n', 'n', 'i', 'n', 'g',
		0x73, 'p', 'e', 'r', 'c', 'e', 'n', 't', 'a', 'g', 'e', '_', 'c', 'o', 'm', 'p', 'l', 'e', 't', 'e',
		0x18, 0x2a, // uint8 42
	}
	pr, ok := ParseProgress(frame)
	if !ok {
		t.Fatalf("valid frame not parsed")
	}
	if pr.Percent != 42 || string(pr.Status) != "running" {
		t.Fatalf("got status=%q percent=%d", pr.Status, pr.Percent)
	}
}
