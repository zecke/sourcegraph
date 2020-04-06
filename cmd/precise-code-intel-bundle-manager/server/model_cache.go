package server

import (
	"sync"

	lru "github.com/hashicorp/golang-lru"
)

//
// TODO - cache based on size rather than number of entries
//

type DocumentDataCache struct{ *simpleCache }

func NewDocumentDataCache(size int) (*DocumentDataCache, error) {
	cache, err := newSimpleCache(size)
	if err != nil {
		return nil, err
	}

	return &DocumentDataCache{cache}, nil
}

func (c *DocumentDataCache) Get(key string, factory func() (DocumentData, error)) (DocumentData, error) {
	value, err := c.simpleCache.Get(key, func() (interface{}, error) { return factory() })
	if err != nil {
		return DocumentData{}, err
	}

	return value.(DocumentData), nil
}

type ResultChunkDataCache struct{ *simpleCache }

func NewResultChunkDataCache(size int) (*ResultChunkDataCache, error) {
	cache, err := newSimpleCache(size)
	if err != nil {
		return nil, err
	}

	return &ResultChunkDataCache{cache}, nil
}

func (c *ResultChunkDataCache) Get(key string, factory func() (ResultChunkData, error)) (ResultChunkData, error) {
	value, err := c.simpleCache.Get(key, func() (interface{}, error) { return factory() })
	if err != nil {
		return ResultChunkData{}, err
	}

	return value.(ResultChunkData), nil
}

type simpleCache struct {
	cacheMu sync.Mutex
	cache   *lru.Cache
}

func newSimpleCache(size int) (*simpleCache, error) {
	cache, err := lru.New(size)
	if err != nil {
		return nil, err
	}

	return &simpleCache{cache: cache}, nil
}

func (c *simpleCache) Get(key string, factory func() (interface{}, error)) (interface{}, error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	if value, exists := c.cache.Get(key); exists {
		return value, nil
	}

	value, err := factory()
	if err != nil {
		return nil, err
	}

	c.cache.Add(key, value)
	return value, nil
}
