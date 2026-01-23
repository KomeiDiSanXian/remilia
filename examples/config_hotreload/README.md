# Configuration Hot-Reload Example

This example demonstrates how to use the configuration hot-reload feature in Remilia.

## Prerequisites

You need a valid configuration file in the current directory. Create a `config.yaml` file:

```yaml
bot:
  app_id: 123456
  bot_id: 654321
  token: "test-token"
  secret: "test-secret"

server:
  host: "0.0.0.0"
  port: 8080

log:
  level: "info"
  format: "text"

concurrency:
  limit: 100
  policy: "drop"
  wait_timeout: "5s"
  event_buffer: 1000

retry:
  enable: true
  max_attempts: 3
  backoff_base: "1s"
  backoff_max: "30s"

middleware:
  logging: true
  recover: true
  auth: false
  rate_limit: false
  metrics: true

dead_letter:
  enable: false

webhook:
  event_buffer: 1000
  dedup_enable: true
  dedup_shards: 16
  dedup_life_window: "5m"
  dedup_clean_window: "1m"
  dedup_max_entry_size: 10000
  dedup_hard_max_size: 100000
```

## Running the Example

```bash
# Build with example tag
go build -tags example -o config_hotreload main.go

# Run
./config_hotreload
```

Or run directly:

```bash
go run -tags example main.go
```

## What It Does

The example shows three usage patterns:

### Example 1: Basic Usage
- Creates a configuration watcher
- Adds a callback to handle log level changes
- Monitors the config file for changes
- Dynamically applies log level updates

### Example 2: Auto-Restart Pattern
- Demonstrates component restart on config change
- Simulates stopping and starting components
- Useful for components that need complete restart

### Example 3: Advanced Validation
- Custom validation beyond basic checks
- Multiple callbacks for different purposes
- Periodic statistics reporting

## Try It Out

1. Start the application
2. Open `config.yaml` in your editor
3. Change the `log.level` from `"info"` to `"debug"`
4. Save the file
5. Watch the application logs - you'll see:
   ```
   [ConfigWatcher] Configuration reloaded successfully
   Log level changed old=info new=debug
   ```

## Key Features Demonstrated

- ✅ File system watching
- ✅ Debounce mechanism (100ms default)
- ✅ Configuration validation
- ✅ Callback execution
- ✅ Dynamic log level adjustment
- ✅ Graceful shutdown

## Documentation

For more details, see:
- [Implementation Guide](../../docs/CONFIG_HOTRELOAD_IMPLEMENTATION.md)
- [Quick Reference](../../docs/CONFIG_HOTRELOAD_QUICKREF.md)
