package limiter

import (
	"fmt"
	"log"
	"math"
	"sync/atomic"
	"time"

	"github.com/garyburd/redigo/redis"
)

type RateLimiter struct {
	RedisPool *redis.Pool

	Limit         uint64
	BaseKey       string
	Interval      time.Duration
	FlushInterval time.Duration

	// Just in case we can't use multi (for example: https://github.com/twitter/twemproxy/blob/master/notes/redis.md)
	MultiSupport bool

	syncedCount  uint64
	currentCount uint64
	currentKey   string

	ticker     *time.Ticker
	stopTicker chan bool
}

// New returns an instance or RateLimiter, which isn't yet initialized
func New(redisPool *redis.Pool, baseKey string, limit uint64, interval time.Duration, flushInterval time.Duration) *RateLimiter {
	rl := &RateLimiter{
		RedisPool: redisPool,

		Limit:         limit,
		BaseKey:       baseKey,
		Interval:      interval,
		FlushInterval: flushInterval,

		MultiSupport: true,
	}

	return rl
}

// Updates the current key, based on the base key
func (rl *RateLimiter) updateCurrentKey() {
	now := float64(time.Now().Unix())

	seconds := rl.Interval.Seconds()

	currentTimeIntervalString := fmt.Sprintf("%d", int64(math.Floor(now/seconds)))

	rl.currentKey = fmt.Sprintf("%s:%s", rl.BaseKey, currentTimeIntervalString)
}

// Stop terminates the ticker, and flushed the final count we had
func (rl *RateLimiter) Stop() {
	close(rl.stopTicker)
	rl.Flush()
}

// Flush increments the counter in Redis, and saves the new total value
func (rl *RateLimiter) Flush() {
	flushCount := atomic.SwapUint64(&rl.currentCount, 0)

	// send to redis, and get the updated value
	redisConn := rl.RedisPool.Get()
	
	// We have to close the connection ourselves when we're done
	defer redisConn.Close()

	var newSyncedCount uint64

	// Just in case we can't use multi (for example: https://github.com/twitter/twemproxy/blob/master/notes/redis.md)
	if rl.MultiSupport == true {
		redisConn.Send("MULTI")
		redisConn.Send("INCRBY", rl.currentKey, flushCount)
		redisConn.Send("EXPIRE", rl.currentKey, rl.Interval.Seconds())
		reply, redisErr := redis.Values(redisConn.Do("EXEC"))

		if redisErr != nil {
			// Could not increment, so restore the current count
			atomic.AddUint64(&rl.currentCount, flushCount)

			log.Printf("Error executing Redis commands: %v", redisErr)
			return
		}

		if _, scanErr := redis.Scan(reply, &newSyncedCount); scanErr != nil {
			log.Printf("Error reading new synced count: %v", scanErr)
			return
		}
	} else {
		reply, incrErr := redis.Uint64(redisConn.Do("INCRBY", rl.currentKey, flushCount))

		if incrErr != nil {
			// Could not increment, so restore the current count
			atomic.AddUint64(&rl.currentCount, flushCount)

			log.Printf("Error executing Redis commands: %v", incrErr)
			return
		}

		newSyncedCount = reply

		_, expireErr := redisConn.Do("EXPIRE", rl.currentKey, rl.Interval.Seconds())
		if expireErr != nil {
			log.Printf("Error calling EXPIRE command: %v", expireErr)
		}
	}

	rl.syncedCount = newSyncedCount
}

// Increment adds 1 to the local counter (doesn't get synced until Flush gets called)
func (rl *RateLimiter) Increment() {
	atomic.AddUint64(&rl.currentCount, 1)
}

// IsOverLimit checks if we are over the limit we have set
func (rl *RateLimiter) IsOverLimit() bool {
	if rl.syncedCount+rl.currentCount > rl.Limit {
		return true
	}

	return false
}

// Init starts the ticker, which takes care of periodically flushing/syncing the counter
func (rl *RateLimiter) Init() error {
	if rl.Interval < time.Minute {
		return fmt.Errorf("Minimum interval is 1 minute")
	}

	rl.updateCurrentKey()

	rl.ticker = time.NewTicker(rl.FlushInterval)

	go func(rl *RateLimiter) {
		for {
			select {
			case <-rl.ticker.C:
				// do stuff
				rl.updateCurrentKey()
				rl.Flush()
			case <-rl.stopTicker:
				log.Printf("Stopping rate limit worker")
				rl.ticker.Stop()
				return
			}
		}
	}(rl)

	return nil
}
