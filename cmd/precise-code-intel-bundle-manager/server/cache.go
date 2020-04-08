package server

import (
	"sync"

	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/internal/memcache"
)

// DatabaseCache is an in-memory LRU cache of Database instances.
type DatabaseCache struct {
	cache memcache.Cache
}

type databaseCacheEntry struct {
	db   *Database
	wg   sync.WaitGroup // user ref count
	once sync.Once      // guards db.Close()
}

// NewDatabaseCache creates a Database instance cache with the given maximum size.
func NewDatabaseCache(size int) (*DatabaseCache, error) {
	cache, err := memcache.NewWithEvict(size, onDatabaseCacheEvict)
	if err != nil {
		return nil, err
	}

	return &DatabaseCache{cache: cache}, nil
}

// WithDatabase invokes the given handler function with a Database instance either
// cached at the given key, or created with the given openDatabase function. This
// method is goroutine-safe and the database instance is guaranteed to remain open
// until the handler has returned, regardless of the cache entry's eviction status.
func (c *DatabaseCache) WithDatabase(key string, openDatabase func() (*Database, error), handler func(db *Database) error) error {
	value, err := c.cache.GetOrCreate(key, func() (interface{}, int, error) {
		db, err := openDatabase()
		if err != nil {
			return nil, 0, err
		}

		entry := &databaseCacheEntry{db: db}
		entry.wg.Add(1)
		return entry, 1, nil
	})
	if err != nil {
		return err
	}

	entry := value.(*databaseCacheEntry)
	defer entry.wg.Done()
	return handler(entry.db)
}

// onDatabaseCacheEvict closest the cached database value after its refcount has
// dropped to zero. The close method is guaranteed to be invoked only once.
func onDatabaseCacheEvict(key interface{}, value interface{}) {
	entry := value.(*databaseCacheEntry)

	entry.once.Do(func() {
		go func() {
			entry.wg.Wait()

			if err := entry.db.Close(); err != nil {
				log15.Error("Failed to close database", "cacheKey", key, "error", err)
			}
		}()
	})
}

// DocumentDataCache is an in-memory LRU cache of unmarshalled DocumentData instances.
type DocumentDataCache struct {
	cache memcache.Cache
}

// NewDocumentDataCache creates a DocumentData instance cache with the given maximum size.
// The size of the cache is determined by the number of field in each DocumentData value.
func NewDocumentDataCache(size int) (*DocumentDataCache, error) {
	cache, err := memcache.New(size)
	if err != nil {
		return nil, err
	}

	return &DocumentDataCache{cache: cache}, nil
}

// GetOrCreate returns the document data cached at the given key or calls the given factory
// to create it. This method is goroutine-safe.
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

// ResultChunkDataCache is an in-memory LRU cache of unmarshalled ResultChunkData instances.
type ResultChunkDataCache struct {
	cache memcache.Cache
}

// ResultChunkDataCache creates a ResultChunkData instance cache with the given maximum size.
// The size of the cache is determined by the number of field in each ResultChunkData value.
func NewResultChunkDataCache(size int) (*ResultChunkDataCache, error) {
	cache, err := memcache.New(size)
	if err != nil {
		return nil, err
	}

	return &ResultChunkDataCache{cache: cache}, nil
}

// GetOrCreate returns the result chunk data cached at the given key or calls the given factory
// to create it. This method is goroutine-safe.
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
