package mjapi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Minimal CBOR (RFC 8949) decoder — just enough to read Midjourney's realtime
// WebSocket frames (maps of scalars + binary image blobs). Avoids a third-party
// dependency. Returns top-level values as map[string]any / []any / string /
// []byte / int64 / float64 / bool / nil.

var errCBOREOF = errors.New("cbor: unexpected end of input")

// CBORDecode decodes a single CBOR item from b.
func CBORDecode(b []byte) (any, error) {
	v, _, err := cborItem(b, 0)
	return v, err
}

func cborItem(b []byte, p int) (any, int, error) {
	if p >= len(b) {
		return nil, p, errCBOREOF
	}
	ib := b[p]
	p++
	mt := ib >> 5
	ai := ib & 0x1f

	if mt == 7 { // simple values & floats
		switch ai {
		case 20:
			return false, p, nil
		case 21:
			return true, p, nil
		case 22, 23:
			return nil, p, nil
		case 25: // float16
			if p+2 > len(b) {
				return nil, p, errCBOREOF
			}
			f := float16to64(binary.BigEndian.Uint16(b[p:]))
			return f, p + 2, nil
		case 26: // float32
			if p+4 > len(b) {
				return nil, p, errCBOREOF
			}
			f := float64(math.Float32frombits(binary.BigEndian.Uint32(b[p:])))
			return f, p + 4, nil
		case 27: // float64
			if p+8 > len(b) {
				return nil, p, errCBOREOF
			}
			f := math.Float64frombits(binary.BigEndian.Uint64(b[p:]))
			return f, p + 8, nil
		default:
			return nil, p, nil
		}
	}

	// argument (length / value) for major types 0-6
	var n uint64
	switch {
	case ai < 24:
		n = uint64(ai)
	case ai == 24:
		if p+1 > len(b) {
			return nil, p, errCBOREOF
		}
		n = uint64(b[p])
		p++
	case ai == 25:
		if p+2 > len(b) {
			return nil, p, errCBOREOF
		}
		n = uint64(binary.BigEndian.Uint16(b[p:]))
		p += 2
	case ai == 26:
		if p+4 > len(b) {
			return nil, p, errCBOREOF
		}
		n = uint64(binary.BigEndian.Uint32(b[p:]))
		p += 4
	case ai == 27:
		if p+8 > len(b) {
			return nil, p, errCBOREOF
		}
		n = binary.BigEndian.Uint64(b[p:])
		p += 8
	default:
		return nil, p, fmt.Errorf("cbor: bad additional info %d", ai)
	}

	// remaining input; compared against n in uint64 so a malformed huge length
	// can't overflow int and bypass a bounds check.
	remaining := uint64(len(b) - p)

	switch mt {
	case 0: // unsigned int
		return int64(n), p, nil
	case 1: // negative int
		return -1 - int64(n), p, nil
	case 2: // byte string
		if n > remaining {
			return nil, p, errCBOREOF
		}
		bs := make([]byte, n)
		copy(bs, b[p:p+int(n)])
		return bs, p + int(n), nil
	case 3: // text string
		if n > remaining {
			return nil, p, errCBOREOF
		}
		s := string(b[p : p+int(n)])
		return s, p + int(n), nil
	case 4: // array
		// Each element needs ≥1 byte, so a declared count beyond the remaining
		// bytes is malformed; cap the pre-alloc to avoid a giant allocation (the
		// loop still errors out on EOF if n was a lie).
		arr := make([]any, 0, capHint(n, remaining))
		for i := uint64(0); i < n; i++ {
			var v any
			var err error
			if v, p, err = cborItem(b, p); err != nil {
				return nil, p, err
			}
			arr = append(arr, v)
		}
		return arr, p, nil
	case 5: // map
		m := make(map[string]any, capHint(n, remaining))
		for i := uint64(0); i < n; i++ {
			k, p2, err := cborItem(b, p)
			if err != nil {
				return nil, p, err
			}
			v, p3, err := cborItem(b, p2)
			if err != nil {
				return nil, p, err
			}
			p = p3
			if ks, ok := k.(string); ok {
				m[ks] = v
			}
		}
		return m, p, nil
	case 6: // tag — decode and return the tagged content
		return cborItem(b, p)
	}
	return nil, p, fmt.Errorf("cbor: unhandled major type %d", mt)
}

// capHint bounds a CBOR-declared element count to what the remaining buffer
// could possibly hold (each element consumes ≥1 byte), so a malformed huge
// count can't trigger a giant pre-allocation.
func capHint(n, remaining uint64) int {
	if n > remaining {
		return int(remaining)
	}
	return int(n)
}

func float16to64(u uint16) float64 {
	sign := uint32(u&0x8000) << 16
	exp := uint32(u>>10) & 0x1f
	mant := uint32(u & 0x3ff)
	switch exp {
	case 0:
		if mant == 0 {
			return float64(math.Float32frombits(sign))
		}
		return float64(math.Float32frombits(sign|((mant)<<13))) / 16384.0
	case 0x1f:
		return float64(math.Float32frombits(sign | 0x7f800000 | (mant << 13)))
	}
	return float64(math.Float32frombits(sign | ((exp + 112) << 23) | (mant << 13)))
}

// Progress is a decoded realtime WebSocket progress frame.
type Progress struct {
	JobID   string
	Status  Status
	Percent int
}

// ParseProgress decodes a CBOR WS frame and extracts job progress. ok is false
// for frames without a job status (e.g. user_success handshake frames). It never
// panics on malformed input — the decoder is overflow-safe, and this recover is
// a last-resort guard so a bad frame can't crash the watch loop.
func ParseProgress(frame []byte) (prog Progress, ok bool) {
	defer func() {
		if recover() != nil {
			prog, ok = Progress{}, false
		}
	}()
	v, err := CBORDecode(frame)
	if err != nil {
		return Progress{}, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return Progress{}, false
	}
	cs, ok := m["current_status"].(string)
	if !ok || cs == "" {
		return Progress{}, false
	}
	p := Progress{Status: Status(cs)}
	if id, ok := m["job_id"].(string); ok {
		p.JobID = id
	}
	// Percent rides under "percentage_complete" (live web key); accept "percent"
	// and any "percent"-prefixed key as a fallback against schema drift.
	pv, found := m["percentage_complete"]
	if !found {
		pv, found = m["percent"]
	}
	if !found {
		for k, v := range m {
			if strings.HasPrefix(k, "percent") {
				pv = v
				break
			}
		}
	}
	switch n := pv.(type) {
	case int64:
		p.Percent = int(n)
	case float64:
		p.Percent = int(n)
	}
	return p, true
}
