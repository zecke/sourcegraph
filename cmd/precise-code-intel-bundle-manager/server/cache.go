package server

import (
	"sync"

	"github.com/sourcegraph/sourcegraph/internal/memcache"
)

type DatabaseCache struct{ cache memcache.Cache }

type databaseCacheEntry struct {
	db   *Database
	wg   sync.WaitGroup
	once sync.Once
}

func NewDatabaseCache(size int) (*DatabaseCache, error) {
	cache, err := memcache.NewWithEvict(size, onDatabaseCacheEvict)
	if err != nil {
		return nil, err
	}

	return &DatabaseCache{cache: cache}, nil
}

func (c *DatabaseCache) WithDatabase(key string, openDatabase func() (*Database, error), handler func(db *Database) error) error {
	value, err := c.cache.GetOrCreate(key, func() (interface{}, int, error) {
		entry, err := newDatabaseCacheEntry(openDatabase)
		return entry, 1, err
	})
	if err != nil {
		return err
	}

	entry := value.(*databaseCacheEntry)
	defer entry.wg.Done()
	return handler(entry.db)
}

func newDatabaseCacheEntry(openDatabase func() (*Database, error)) (*databaseCacheEntry, error) {
	db, err := openDatabase()
	if err != nil {
		return nil, err
	}

	entry := &databaseCacheEntry{db: db}
	entry.wg.Add(1)
	return entry, nil
}

func onDatabaseCacheEvict(key interface{}, value interface{}) {
	entry := value.(*databaseCacheEntry)

	entry.once.Do(func() {
		go func() {
			entry.wg.Wait()
			_ = entry.db.Close() // TODO - handle error
		}()
	})
}

type DocumentDataCache struct{ cache memcache.Cache }

func NewDocumentDataCache(size int) (*DocumentDataCache, error) {
	cache, err := memcache.New(size)
	if err != nil {
		return nil, err
	}

	return &DocumentDataCache{cache: cache}, nil
}

func (c *DocumentDataCache) GetOrCreate(key string, factory func() (DocumentData, error)) (DocumentData, error) {
	value, err := c.cache.GetOrCreate(key, func() (interface{}, int, error) {
		data, err := factory()
		if err != nil {
			return data, 0, err
		}

		return data, 1 + len(data.HoverResults) + len(data.Monikers) + len(data.PackageInformation) + len(data.Ranges), err
	})
	if err != nil {
		return DocumentData{}, err
	}

	return value.(DocumentData), nil
}

type ResultChunkDataCache struct{ cache memcache.Cache }

func NewResultChunkDataCache(size int) (*ResultChunkDataCache, error) {
	cache, err := memcache.New(size)
	if err != nil {
		return nil, err
	}

	return &ResultChunkDataCache{cache: cache}, nil
}

func (c *ResultChunkDataCache) GetOrCreate(key string, factory func() (ResultChunkData, error)) (ResultChunkData, error) {
	value, err := c.cache.GetOrCreate(key, func() (interface{}, int, error) {
		data, err := factory()
		if err != nil {
			return data, 0, err
		}

		return data, 1 + len(data.DocumentPaths) + len(data.DocumentIDRangeIDs), err
	})
	if err != nil {
		return ResultChunkData{}, err
	}

	return value.(ResultChunkData), nil
}
