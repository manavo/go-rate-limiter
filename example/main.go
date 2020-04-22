package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gomodule/redigo/redis"
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
