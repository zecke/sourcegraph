package server

import (
	"sync"
	"testing"
)

func TestSimpleCache(t *testing.T) {
	cache, err := newSimpleCache(3)
	if err != nil {
		t.Fatalf("unexpected error creating cache: %s", err)
	}

	numFactoryCalls := 0

	assertKeyValue := func(key string) {
		value, err := cache.Get(key, func() (interface{}, error) {
			numFactoryCalls++
			return key, nil
		})

		if err != nil {
			t.Fatalf("unexpected error fetching value: %s", err)
		} else if value != key {
			t.Errorf("unexpected cache result for %s: want=%s have=%s", key, key, value)
		}
	}

	assertKeyValue("foo") // foo
	assertKeyValue("bar") // bar foo
	assertKeyValue("baz") // baz bar foo

	if numFactoryCalls != 3 {
		t.Errorf("unexpected number of factory calls: want=%d have=%d", 3, numFactoryCalls)
	}

	assertKeyValue("foo") // foo baz bar
	assertKeyValue("bar") // bar foo baz
	assertKeyValue("baz") // baz bar foo

	if numFactoryCalls != 3 {
		t.Errorf("unexpected number of factory calls: want=%d have=%d", 3, numFactoryCalls)
	}

	assertKeyValue("bonk") // bonk baz bar
	assertKeyValue("quux") // quux bonk baz
	assertKeyValue("baz")  // baz quux bonk

	if numFactoryCalls != 5 {
		t.Errorf("unexpected number of factory calls: want=%d have=%d", 5, numFactoryCalls)
	}

	assertKeyValue("foo") // foo bonk baz
	assertKeyValue("bar") // bar foo quux

	if numFactoryCalls != 7 {
		t.Errorf("unexpected number of factory calls: want=%d have=%d", 7, numFactoryCalls)
	}
}

type cacheResult struct {
	value interface{}
	err   error
}

func TestSimpleCacheConcurrency(t *testing.T) {
	cache, err := newSimpleCache(3)
	if err != nil {
		t.Fatalf("unexpected error creating cache: %s", err)
	}

	numFactoryCalls := 0
	lockCh := make(chan struct{}, 1)
	blockCh := make(chan struct{})

	factory := func() (interface{}, error) {
		lockCh <- struct{}{} // cache lock signal
		<-blockCh            // block until test runner frees us
		numFactoryCalls++
		return "released", nil
	}

	var wg sync.WaitGroup
	values := make(chan cacheResult, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			value, err := cache.Get("test", factory)
			values <- cacheResult{value, err}
		}()
	}

	<-lockCh       // wait until cache is locked
	close(blockCh) // unblock factory invocation
	wg.Wait()      // wait for all values to be written
	close(values)  // cleanup

	for result := range values {
		if result.err != nil {
			t.Fatalf("unexpected error fetching value: %s", err)
		} else if result.value != "released" {
			t.Errorf("unexpected cache result: want=%s have=%s", "release", result.value)
		}
	}

	if numFactoryCalls != 1 {
		t.Errorf("unexpected number of factory calls: want=%d have=%d", 1, numFactoryCalls)
	}
}
