# Config Test Fix Report - LOG_LEVEL Case Sensitivity

**Date:** 2026-02-12  
**Test:** `TestLoadDefault/load_from_environment_variables`  
**Status:** ✅ FIXED

---

## Problem Description

The test `TestLoadDefault/load_from_environment_variables` was failing with the following error:

```
Error: no config file found and environment variables incomplete: 
       invalid log config: log.level must be one of [debug, info, warn, error, fatal, panic], got 'DEBUG'
```

### Root Cause

The configuration validation for log level is **case-sensitive** and only accepts lowercase values:
- ✅ Valid: `debug`, `info`, `warn`, `error`, `fatal`, `panic`
- ❌ Invalid: `DEBUG`, `INFO`, `WARN`, etc.

However, when loading configuration from environment variables, the system was reading the `LOG_LEVEL` environment variable directly without normalizing it to lowercase. In the test environment, `LOG_LEVEL` was set to `DEBUG` (uppercase), causing the validation to fail.

---

## Solution

Normalize the log level and format to lowercase when loading from environment variables.

### Code Changes

**File:** `config/config.go`

**Before:**
```go
Log: LogConfig{
    Level:  getEnvDefault("LOG_LEVEL", "info"),
    Format: getEnvDefault("LOG_FORMAT", "text"),
},
```

**After:**
```go
Log: LogConfig{
    Level:  strings.ToLower(getEnvDefault("LOG_LEVEL", "info")),
    Format: strings.ToLower(getEnvDefault("LOG_FORMAT", "text")),
},
```

### Why This Fix Works

1. **User-Friendly**: Accepts environment variables in any case (DEBUG, debug, Debug all work)
2. **Backward Compatible**: Lowercase values still work as before
3. **Consistent**: Aligns with common practice where environment variables are case-insensitive for enum values
4. **Safe**: The `strings.ToLower()` function is safe to use on empty strings and returns the same value

---

## Testing Results

### Before Fix
```
--- FAIL: TestLoadDefault/load_from_environment_variables (0.00s)
    load_test.go:180: 
        Error: invalid log config: log.level must be one of 
               [debug, info, warn, error, fatal, panic], got 'DEBUG'
FAIL
```

### After Fix
```
=== RUN   TestLoadDefault
=== RUN   TestLoadDefault/load_from_config.yaml
=== RUN   TestLoadDefault/load_from_environment_variables
=== RUN   TestLoadDefault/no_config_file_and_incomplete_env
--- PASS: TestLoadDefault (0.02s)
    --- PASS: TestLoadDefault/load_from_config.yaml (0.01s)
    --- PASS: TestLoadDefault/load_from_environment_variables (0.00s)
    --- PASS: TestLoadDefault/no_config_file_and_incomplete_env (0.00s)
PASS
ok      github.com/KomeiDiSanXian/remilia/config        0.504s
```

### All Config Tests
```
✅ All tests pass
ok      github.com/KomeiDiSanXian/remilia/config        2.598s
```

---

## Impact

### ✅ Benefits

1. **Improved User Experience**: Users can now set environment variables in any case
   - `LOG_LEVEL=DEBUG` ✅ Works
   - `LOG_LEVEL=debug` ✅ Works
   - `LOG_LEVEL=Debug` ✅ Works

2. **More Robust**: Handles environment variables from different shells/systems that may use different conventions

3. **Consistent Behavior**: Both log level and format are now case-insensitive

### 🔍 No Breaking Changes

- Existing configurations with lowercase values continue to work
- YAML/JSON config files are unaffected (they typically use lowercase)
- Only affects environment variable loading

---

## Related Configuration

The following environment variables are now case-insensitive:

| Variable | Values | Example |
|----------|--------|---------|
| `LOG_LEVEL` | debug, info, warn, error, fatal, panic | `LOG_LEVEL=DEBUG` |
| `LOG_FORMAT` | text, json | `LOG_FORMAT=JSON` |

---

## Additional Validation

The configuration validation remains unchanged and still enforces valid values:

```go
func (lc *LogConfig) Validate() error {
    validLevels := map[string]bool{
        "debug": true, "info": true, "warn": true, 
        "error": true, "fatal": true, "panic": true,
    }
    if lc.Level != "" && !validLevels[lc.Level] {
        return fmt.Errorf("log.level must be one of [debug, info, warn, error, fatal, panic], got '%s'", lc.Level)
    }
    // ...
}
```

The normalization happens **before** validation, ensuring:
1. Environment variables are normalized to lowercase
2. Validation checks against lowercase values
3. Invalid values (after normalization) still fail validation

---

## Edge Cases Handled

| Input | Normalized | Validation |
|-------|-----------|------------|
| `DEBUG` | `debug` | ✅ Pass |
| `Debug` | `debug` | ✅ Pass |
| `debug` | `debug` | ✅ Pass |
| `INVALID` | `invalid` | ❌ Fail (as expected) |
| `""` (empty) | `""` | ✅ Pass (uses default: "info") |

---

## Best Practices

### For Users

**Environment Variables:**
```bash
# All of these work now
export LOG_LEVEL=DEBUG
export LOG_LEVEL=debug
export LOG_LEVEL=Debug

export LOG_FORMAT=JSON
export LOG_FORMAT=json
```

**Config Files (unchanged):**
```yaml
log:
  level: debug  # Use lowercase in config files (convention)
  format: text
```

### For Developers

When adding new enum-type configuration options that can be set via environment variables:

1. **Normalize to lowercase** when reading from environment variables
2. **Validate against lowercase** values
3. **Document** accepted values in lowercase

```go
// Good example
SomeEnum: strings.ToLower(getEnvDefault("SOME_ENUM", "default")),
```

---

## Files Modified

1. `config/config.go` - Added `strings.ToLower()` for log level and format

---

## Conclusion

This fix makes the configuration system more user-friendly and robust by accepting environment variables in any case while maintaining strict validation. All tests now pass successfully.

**Status:** ✅ RESOLVED  
**Tests:** ✅ ALL PASSING  
**Breaking Changes:** ❌ NONE

