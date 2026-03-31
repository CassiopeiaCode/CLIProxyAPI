package chat_completions

import (
	"container/list"
	"crypto/sha256"
	"encoding/json"
	"sync"
)

const parsedRequestCacheSize = 256

type parsedRequestCacheEntry struct {
	key [32]byte
	req chatReqInput
}

type parsedRequestCache struct {
	mu    sync.Mutex
	order *list.List
	items map[[32]byte]*list.Element
}

var openAIRequestCache = newParsedRequestCache(parsedRequestCacheSize)

func newParsedRequestCache(size int) *parsedRequestCache {
	if size <= 0 {
		size = parsedRequestCacheSize
	}
	return &parsedRequestCache{
		order: list.New(),
		items: make(map[[32]byte]*list.Element, size),
	}
}

func PrimeOpenAIRequest(rawJSON []byte) {
	if len(rawJSON) == 0 {
		return
	}
	cacheKey := requestCacheKey(rawJSON)
	if _, ok := openAIRequestCache.get(cacheKey); ok {
		return
	}
	var req chatReqInput
	if err := json.Unmarshal(rawJSON, &req); err != nil {
		return
	}
	openAIRequestCache.put(cacheKey, req)
}

func cachedOpenAIRequest(rawJSON []byte) (chatReqInput, bool) {
	if len(rawJSON) == 0 {
		return chatReqInput{}, false
	}
	return openAIRequestCache.get(requestCacheKey(rawJSON))
}

func requestCacheKey(rawJSON []byte) [32]byte {
	return sha256.Sum256(rawJSON)
}

func (c *parsedRequestCache) get(key [32]byte) (chatReqInput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return chatReqInput{}, false
	}
	c.order.MoveToFront(elem)
	entry, ok := elem.Value.(*parsedRequestCacheEntry)
	if !ok || entry == nil {
		return chatReqInput{}, false
	}
	return entry.req, true
}

func (c *parsedRequestCache) put(key [32]byte, req chatReqInput) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		if entry, ok := elem.Value.(*parsedRequestCacheEntry); ok && entry != nil {
			entry.req = req
		}
		return
	}

	elem := c.order.PushFront(&parsedRequestCacheEntry{key: key, req: req})
	c.items[key] = elem
	if c.order.Len() <= parsedRequestCacheSize {
		return
	}

	tail := c.order.Back()
	if tail == nil {
		return
	}
	c.order.Remove(tail)
	entry, ok := tail.Value.(*parsedRequestCacheEntry)
	if !ok || entry == nil {
		return
	}
	delete(c.items, entry.key)
}
