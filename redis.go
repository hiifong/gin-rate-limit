package ratelimit

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var rateLimitScript = redis.NewScript(`
local key_ts   = KEYS[1]
local key_hits = KEYS[2]
local rate     = tonumber(ARGV[1])
local limit    = tonumber(ARGV[2])
local now      = tonumber(ARGV[3])

local ts   = tonumber(redis.call('GET', key_ts))   or now
local hits = tonumber(redis.call('GET', key_hits)) or 0

if ts + rate <= now then
    hits = 0
end

if hits >= limit then
    return {ts, hits, 1}
end

hits = hits + 1
redis.call('SET',    key_ts,   now)
redis.call('SET',    key_hits, hits)
redis.call('EXPIRE', key_ts,   rate * 2)
redis.call('EXPIRE', key_hits, rate * 2)

return {ts, hits, 0}
`)

type redisStoreType struct {
	rate       int64
	limit      uint
	client     *redis.Client
	panicOnErr bool
	skip       func(c *gin.Context) bool
}

func (s *redisStoreType) Limit(key string, c *gin.Context) Info {
	now := time.Now().Unix()
	res, err := rateLimitScript.Run(
		c.Request.Context(),
		s.client,
		[]string{key + "ts", key + "hits"},
		s.rate, s.limit, now,
	).Slice()
	if err != nil {
		if s.panicOnErr {
			panic(err)
		}
		return Info{
			Limit:         s.limit,
			RateLimited:   false,
			ResetTime:     time.Now().Add(time.Duration(s.rate) * time.Second),
			RemainingHits: s.limit,
		}
	}

	ts          := res[0].(int64)
	hits        := res[1].(int64)
	rateLimited := res[2].(int64) == 1
	resetTime   := time.Now().Add(time.Duration(s.rate-(now-ts)) * time.Second)

	if s.skip != nil && s.skip(c) {
		return Info{
			Limit:         s.limit,
			RateLimited:   false,
			ResetTime:     resetTime,
			RemainingHits: s.limit - uint(hits),
		}
	}

	remaining := uint(0)
	if !rateLimited {
		remaining = s.limit - uint(hits)
	}
	return Info{
		Limit:         s.limit,
		RateLimited:   rateLimited,
		ResetTime:     resetTime,
		RemainingHits: remaining,
	}
}

type RedisOptions struct {
	// the user can make Limit amount of requests every Rate
	Rate time.Duration
	// the amount of requests that can be made every Rate
	Limit       uint
	RedisClient *redis.Client
	// should gin-rate-limit panic when there is an error with redis
	PanicOnErr bool
	// a function that returns true if the request should not count toward the rate limit
	Skip func(*gin.Context) bool
}

func RedisStore(options *RedisOptions) Store {
	return &redisStoreType{
		client:     options.RedisClient,
		rate:       int64(options.Rate.Seconds()),
		limit:      options.Limit,
		panicOnErr: options.PanicOnErr,
		skip:       options.Skip,
	}
}
