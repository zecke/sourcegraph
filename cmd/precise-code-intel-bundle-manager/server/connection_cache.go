package server

import (
	"sync"

	lru "github.com/hashicorp/golang-lru"
)

type DatabaseCache struct {
	cacheMu sync.Mutex
	cache   *lru.Cache
}

type databaseCacheEntry struct {
	db   *Database
	wg   sync.WaitGroup
	once sync.Once
}

func NewDatabaseCache(size int) (*DatabaseCache, error) {
	cache, err := lru.NewWithEvict(size, onEvict)
	if err != nil {
		return nil, err
	}

	return &DatabaseCache{cache: cache}, nil
}

func (c *DatabaseCache) WithDatabase(key string, openDatabase func() (*Database, error), handler func(db *Database) error) error {
	entry, err := c.getCacheEntry(key, openDatabase)
	if err != nil {
		return err
	}
	defer entry.wg.Done()

	return handler(entry.db)
}

func (c *DatabaseCache) getCacheEntry(key string, openDatabase func() (*Database, error)) (*databaseCacheEntry, error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	if rawEntry, exists := c.cache.Get(key); exists {
		entry := rawEntry.(*databaseCacheEntry)
		entry.wg.Add(1)
		return entry, nil
	}

	db, err := openDatabase()
	if err != nil {
		return nil, err
	}

	entry := &databaseCacheEntry{db: db}
	entry.wg.Add(1)
	c.cache.Add(key, entry)
	return entry, nil
}

func onEvict(key interface{}, value interface{}) {
	entry := value.(*databaseCacheEntry)

	entry.once.Do(func() {
		go func() {
			entry.wg.Wait()
			_ = entry.db.Close() // TODO - handle error
		}()
	})
}
