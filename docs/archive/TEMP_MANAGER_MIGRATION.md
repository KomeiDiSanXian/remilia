# TempMatcher Migration & Engine Separation

**Date:** 2026-01-07
**Status:** Completed

## 1. Goal
Separate temporary matchers (one-time or time-limited) from the persistent matcher state to improve Engine performance and prevent frequent Copy-On-Write (COW) triggers during high-throughput scenarios.

## 2. Changes

### 2.1 Engine Architecture
- **Separated Storage:**
    - `engineState.matchers`: Only stores persistent matchers (long-living). Managed via COW.
    - `Engine.tempManager`: Stores temporary matchers. Managed via concurrent-safe heap + map, NO COW.
- **Event Processing:**
    - `Engine.ProcessEvent` now retrieves matchers from both sources:
        - `state.getMatchersForEvent(eventType)` (Cached, O(1))
        - `tempManager.Get(eventType)` (Concurrent Map, O(1))
    - Results are merged and sorted by priority.

### 2.2 TempManager Implementation
- **Data Structure:**
    - `map[dto.EventType]*sync.Map`: Fast lookups for event processing.
    - `startHeap`: Min-heap for expiration cleanup (O(1) access to identifying expired items).
- **Graceful Migration:**	
    - `Matcher.SetTemp(true)` automatically moves a matcher from `Engine.state` to `TempManager`.
    - `Matcher.SetTemp(false)` moves it back to `Engine.state`.
    - This allows dynamic promotion/demotion of matchers without breaking existing code.

### 2.3 Cleanup Mechanism
- **Dedicated Cleaner:** `Engine.tempMatcherCleaner` (goroutine) runs periodically.
- **Efficiency:** 
    - Cleaner only acquires locks on the efficient heap to find expired items.
    - Removing items from `TempManager` does NOT trigger full Engine COW, significantly reducing GC pressure.

## 3. Impact
- **Performance:** Adding/removing temp matchers no longer rebuilds the entire Engine matcher index.
- **Safety:** Independent locking strategy prevents lock contention between long-running persistent config changes and high-frequency temp matcher usage (e.g., conversation state machines).
- **Compatibility:** Public API `SetTemp`, `SetTempWithTimeout`, `SetTempWithMaxUse` remains unchanged but now routes to the new storage backend.

## 4. Verification
- **Unit Tests:** `engine_auto_cleaner_test.go`, `temp_matcher_test.go` verified correct behavior.
- **Integration:** Bot e2e tests confirm conversation flows still work.
- **Race Detection:** `go test -race` passed, confirming safe concurrent access.

## 5. Removed Artifacts
- Deleted legacy cleaner tests (`legacy_cleaner_test.go`, `cleaner_panic_test.go`) that relied on implementation details of the old slice-based cleaner.

