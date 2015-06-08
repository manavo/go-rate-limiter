# Go Rate limiter

This package allows us to have a distributed rate limiter, using Redis as a central counter.

The limits that are set are only "soft" limits, and you might end up going a bit over.

## How it works

You create an instance of the rate limiter, by specifying a limit and an interval. For example, maybe you want 2000 requests per hour. Or 25000 requests per day.

When initializing, you specify a "base key". This is used in Redis as the key where we keep count of the total number of requests, across all instances.

You also specify a "flush interval", when the library will increment the Redis counter by the number each instance has counted (since the last sync) and read back the new total.

## Dependencies

This package depends on [garyburd/redigo](https://github.com/garyburd/redigo).

## Example

In the example below, we create a new limiter. We'll be counting the total number of requests, limiting the total to 5 within 1 minute. We'll be syncing with the Redis counter every 5 seconds.

```go
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/manavo/go-rate-limiter"
)

type requestObject struct {
	RateLimiter *limiter.RateLimiter
}

func main() {
	redisPool := &redis.Pool{
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", "127.0.0.1:6379")
			if err != nil {
				return nil, err
			}
			return c, err
		},
	}

	mux := http.NewServeMux()

	request := &requestObject{}

	rl := limiter.New(redisPool, "requests", 5, time.Minute, 5*time.Second)
	limiterErr := rl.Init()

	if limiterErr == nil {
		// request is a "requestObject" instance, that http.NewServeMux().Handle() accepts
		request.RateLimiter = rl
	} else {
		log.Printf("Error creating Rate Limiter: %v", limiterErr)
	}

	mux.Handle(
		"/rate",
		request,
	)

	httpserver := &http.Server{
		Addr:         "0.0.0.0:8888",
		Handler:      mux,
		ReadTimeout:  5000 * time.Millisecond,
		WriteTimeout: 5000 * time.Millisecond,
	}

	httpserver.ListenAndServe()
}

func (req *requestObject) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if req.RateLimiter != nil {
		req.RateLimiter.Increment()

		if req.RateLimiter.IsOverLimit() {
			w.Header().Set("Content-Type", "text/plain")

			// Status code 429: Too many requests
			w.WriteHeader(429)

			w.Write([]byte("Error: Reached limit of requests"))

			return
		}
	}

	w.Write([]byte("Request OK"))
}
```
