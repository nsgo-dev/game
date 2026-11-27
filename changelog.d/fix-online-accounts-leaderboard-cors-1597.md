### Fixed

- `pkg/online/accounts` and `pkg/online/leaderboard/selfhost`: add `allowedOrigins` YAML configuration and shared CORS handling (`pkg/online/internal/cors`) matching `pkg/online/cloudsave`, so web builds served from another origin can complete preflight requests, sign in, refresh tokens, and submit or query leaderboard scores (#1597).
