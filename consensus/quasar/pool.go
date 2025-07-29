package quasar

import (
    "sync"
    "time"
)

// Pool keeps <N> ready-made Ringtail shares in RAM per validator.
type Pool struct {
    mu     sync.Mutex
    cache  []Share
    sk     SecretKey
    target int
}

func NewPool(sk SecretKey, warm int) *Pool {
    p := &Pool{sk: sk, target: warm}
    go p.refill() // background goroutine
    return p
}

// Get pops one share or blocks until available.
func (p *Pool) Get() Share {
    for {
        p.mu.Lock()
        if n := len(p.cache); n > 0 {
            s := p.cache[n-1]
            p.cache = p.cache[:n-1]
            p.mu.Unlock()
            return s
        }
        p.mu.Unlock()
        time.Sleep(time.Millisecond) // backoff
    }
}

// refill pre-computes until len(cache)==target.
func (p *Pool) refill() {
    for {
        p.mu.Lock()
        if len(p.cache) < p.target {
            share, _ := Precompute(p.sk) // fast, no entropy
            p.cache = append(p.cache, share)
            p.mu.Unlock()
            continue
        }
        p.mu.Unlock()
        time.Sleep(2 * time.Millisecond)
    }
}