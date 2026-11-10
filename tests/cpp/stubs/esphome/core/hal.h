#pragma once
// Test stub of the ESPHome 2026.10.1 esphome/core/hal.h: a clock the test sets by hand
// (esphome::stub::now_ms), so reconnect timing is exact and no test ever sleeps. Never used by a
// firmware build.
#include <cstdint>

namespace esphome {

namespace stub {
inline uint32_t now_ms = 1000;
}  // namespace stub

inline uint32_t millis() { return stub::now_ms; }
inline uint64_t millis_64() { return static_cast<uint64_t>(stub::now_ms); }

}  // namespace esphome
