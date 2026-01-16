# Rate Limiter Service (Go)

A fast, scalable rate‑limiter service with multiple algorithms and a pluggable backend
(in‑memory or Redis). Designed to be embedded into any stack via a simple HTTP API.

## Features

- Multiple algorithms: token bucket, leaky bucket, fixed window, sliding log, sliding counter
- Flexible keying: user ID, device ID, JWT, or explicit key
- Redis backend with Lua scripts for atomicity and horizontal scaling
- Simple HTTP interface and predictable headers for downstream services

## Quickstart

```bash
cd /Users/eloelo/grinding/rate-limiter-service
go mod tidy
go run ./cmd/server
```

### Configuration

Environment variables:

- `PORT` (default: `8080`)
- `BACKEND` (`memory` or `redis`, default: `memory`)
- `REDIS_ADDR` (default: `127.0.0.1:6379`)
- `REDIS_PASSWORD` (default: empty)
- `REDIS_DB` (default: `0`)

## API

### POST `/v1/limit/check`

You can provide a direct `key`, or let the service derive one:

- `user_id` → `user:{id}`
- `device_id` → `device:{id}`
- `jwt` or `Authorization: Bearer <token>` → `jwt:{sha256(token)}`

Precedence: `key` > `user_id` > `device_id` > `jwt`.

#### Token bucket

```json
{
  "key": "user:123",
  "algorithm": "token_bucket",
  "capacity": 10,
  "refill_per_sec": 5,
  "cost": 1
}
```

#### Leaky bucket

```json
{
  "key": "user:123",
  "algorithm": "leaky_bucket",
  "capacity": 10,
  "leak_per_sec": 5,
  "cost": 1
}
```

#### Fixed window

```json
{
  "key": "user:123",
  "algorithm": "fixed_window",
  "limit": 100,
  "window_ms": 60000,
  "cost": 1
}
```

#### Sliding window log

```json
{
  "key": "user:123",
  "algorithm": "sliding_window_log",
  "limit": 100,
  "window_ms": 60000,
  "cost": 1
}
```

#### Sliding window counter

```json
{
  "key": "user:123",
  "algorithm": "sliding_window_counter",
  "limit": 100,
  "window_ms": 60000,
  "cost": 1
}
```

#### User / Device / JWT keying

```json
{
  "user_id": "123",
  "algorithm": "fixed_window",
  "limit": 100,
  "window_ms": 60000
}
```

```json
{
  "device_id": "device-abc",
  "algorithm": "sliding_window_counter",
  "limit": 50,
  "window_ms": 60000
}
```

```json
{
  "jwt": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "algorithm": "token_bucket",
  "capacity": 10,
  "refill_per_sec": 5
}
```

### Response (all algorithms)

```json
{
  "key": "user:123",
  "algorithm": "fixed_window",
  "allowed": true,
  "remaining": 99,
  "reset_at_ms": 1737060000000,
  "retry_after_ms": 0
}
```

HTTP status:

- `200` when allowed
- `429` when rate limited
- `400` for invalid input
- `500` for backend errors

Headers:

- `X-RateLimit-Remaining`
- `X-RateLimit-Reset-Ms`
- `X-RateLimit-Retry-After-Ms`

### Health

`GET /healthz`

## Integration Pattern

Call the API before performing protected work. If the response is `allowed=false` or
status `429`, reject or delay the request in your service.

Example (curl):

```bash
curl -s -X POST http://127.0.0.1:8080/v1/limit/check \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"123","algorithm":"fixed_window","limit":100,"window_ms":60000}'
```

## Algorithms Overview

- **Token bucket**: bursty traffic with steady refill
- **Leaky bucket**: smooth output rate
- **Fixed window**: simple counter per time window
- **Sliding window log**: precise, higher memory
- **Sliding window counter**: approximate, lower memory

## Latency Benchmark (local)

Run the server, then:

```bash
go run ./cmd/bench -duration=10s -qps=200 -concurrency=8
```

You can pass key selectors:

- `-key=user:123`
- `-user_id=123`
- `-device_id=device-abc`
- `-jwt=<token>`

## Scaling Notes

- Use `BACKEND=redis` for multiple instances and shared limits.
- Keep Redis close to the service to minimize latency.
- Sliding log accuracy comes with higher memory and latency cost.

## Security Notes

- JWTs are not validated here; they are only used for keying.
- Use a trusted auth service if you need token verification.

## Contributing

Issues and PRs welcome. Please keep changes focused and include tests where possible.

## License

Add your preferred open‑source license (MIT/Apache‑2.0/etc).
