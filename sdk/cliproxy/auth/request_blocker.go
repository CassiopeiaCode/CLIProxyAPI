package auth

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type blockedRequestLRU struct {
	mu      sync.Mutex
	maxSize int
	items   map[string]*list.Element
	order   *list.List
}

func newBlockedRequestLRU(maxSize int) *blockedRequestLRU {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &blockedRequestLRU{
		maxSize: maxSize,
		items:   make(map[string]*list.Element, maxSize),
		order:   list.New(),
	}
}

func (l *blockedRequestLRU) Add(hash string) {
	if l == nil || hash == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if elem, ok := l.items[hash]; ok {
		l.order.MoveToFront(elem)
		return
	}
	elem := l.order.PushFront(hash)
	l.items[hash] = elem
	if l.order.Len() <= l.maxSize {
		return
	}
	tail := l.order.Back()
	if tail == nil {
		return
	}
	l.order.Remove(tail)
	delete(l.items, tail.Value.(string))
}

func (l *blockedRequestLRU) Contains(hash string) bool {
	if l == nil || hash == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	elem, ok := l.items[hash]
	if !ok {
		return false
	}
	l.order.MoveToFront(elem)
	return true
}

func (l *blockedRequestLRU) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}

func requestBodyHash(payload []byte) (string, bool) {
	if len(payload) == 0 {
		return "", false
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), true
}

func requestBodyHashFromOptions(opts cliproxyexecutor.Options) (cliproxyexecutor.Options, string, bool) {
	if analysis, ok := requestBodyAnalysisFromMetadata(opts.Metadata); ok {
		if len(analysis.canonicalJSON) > 0 {
			hash, ok := requestBodyHash(analysis.canonicalJSON)
			return opts, hash, ok
		}
	}
	updated, analysis, ok := ensureRequestBodyAnalysisMetadata(opts)
	if ok && analysis != nil && len(analysis.canonicalJSON) > 0 {
		hash, ok := requestBodyHash(analysis.canonicalJSON)
		return updated, hash, ok
	}
	hash, ok := requestBodyHash(opts.OriginalRequest)
	return updated, hash, ok
}
