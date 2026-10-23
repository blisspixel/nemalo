// Package decodetest provides a synthetic decoder dependency. It runs no
// executable and checks that the service prepared the expected private source.
package decodetest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/blisspixel/nemalo/internal/decode"
)

type PCM struct {
	Source, Data []byte
	Calls        atomic.Int32
}

func NewPCM(source []byte) *PCM {
	p := &PCM{Source: bytes.Clone(source), Data: make([]byte, 3000)}
	for i := range p.Data {
		p.Data[i] = byte(i)
	}
	return p
}
func (p *PCM) Decode(ctx context.Context, path, format string, size int64, q decode.PCMRequest) (decode.PCMRange, error) {
	p.Calls.Add(1)
	if err := ctx.Err(); err != nil {
		return decode.PCMRange{}, err
	}
	if filepath.Base(path) != "asset" || !filepath.IsAbs(path) || format != "m4a" || size != int64(len(p.Source)) {
		return decode.PCMRange{}, fmt.Errorf("decoder did not receive a controlled snapshot")
	}
	f, err := os.Open(path)
	if err != nil {
		return decode.PCMRange{}, err
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(len(p.Source))+1))
	closed := f.Close()
	if err != nil {
		return decode.PCMRange{}, err
	}
	if closed != nil {
		return decode.PCMRange{}, closed
	}
	if !bytes.Equal(b, p.Source) {
		return decode.PCMRange{}, fmt.Errorf("private source differs")
	}
	total := int64(len(p.Data) / 2)
	out := decode.PCMRange{Measurement: decode.Result{Tool: "ffmpeg", Status: "passed", Version: "synthetic/1", SampleRate: 1000, Channels: 2, DecodedSamples: total}, FullSHA256: fmt.Sprintf("%x", sha256.Sum256(p.Data))}
	if q.UnitsOnly {
		return out, nil
	}
	if q.FirstSample >= total {
		return decode.PCMRange{Measurement: out.Measurement}, decode.ErrPCMRange
	}
	out.FirstSample = q.FirstSample
	out.NextSample = min(q.FirstSample+int64(q.MaxBytes/2), total)
	out.EndOfStream = out.NextSample == total
	out.Data = bytes.Clone(p.Data[q.FirstSample*2 : out.NextSample*2])
	return out, nil
}
