#pragma once
// Test stub of the ESPHome 2026.10.1 esphome/core/log.h: the ESP_LOG* macros sim_bus.cpp uses,
// routed to esphome::stub::log, which the test defines (it keeps the warnings for assertions).
// The printf attribute keeps -Wformat checking the hub's format strings. Never used by a firmware
// build.
namespace esphome::stub {
void log(char level, const char *tag, const char *fmt, ...) __attribute__((format(printf, 3, 4)));
}  // namespace esphome::stub

#define ESP_LOGE(tag, ...) ::esphome::stub::log('E', tag, __VA_ARGS__)
#define ESP_LOGW(tag, ...) ::esphome::stub::log('W', tag, __VA_ARGS__)
#define ESP_LOGI(tag, ...) ::esphome::stub::log('I', tag, __VA_ARGS__)
#define ESP_LOGD(tag, ...) ::esphome::stub::log('D', tag, __VA_ARGS__)
#define ESP_LOGV(tag, ...) ::esphome::stub::log('V', tag, __VA_ARGS__)
#define ESP_LOGCONFIG(tag, ...) ::esphome::stub::log('C', tag, __VA_ARGS__)
