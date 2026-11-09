#pragma once
// Sim-bus wire codec, header-only. The grammar, the error classes and the number rules are
// specified in protocol/PROTOCOL.md and pinned by protocol/vectors/frames.tsv, which the Go codec
// (internal/bus/codec.go) and tests/cpp/test_protocol.cpp both run.
//
// Standard library only: no ESPHome includes, no exceptions, no iostreams. Everything is inline in
// namespace esphome::sim_bus::proto, so several translation units can include it. Numbers are
// parsed with strtof, which follows the C locale (a host firmware never calls setlocale).
#include <charconv>
#include <cmath>
#include <cstdint>
#include <cstdlib>
#include <cstring>
#include <string>
#include <vector>

namespace esphome::sim_bus::proto {

constexpr int PROTO = 1;
constexpr size_t MAX_LINE = 4096;       // including the newline
constexpr size_t MAX_NUMBER_LEN = 32;   // characters in a number literal

struct Field {
  std::string key;
  bool is_str;
  std::string text;  // decoded string, or the number literal as written
};

struct Frame {
  std::vector<Field> fields;

  const Field *find(const std::string &key) const {
    for (const auto &f : this->fields)
      if (f.key == key) return &f;
    return nullptr;
  }
  std::string kind() const {
    const Field *f = this->find("k");
    return (f != nullptr && f->is_str) ? f->text : std::string();
  }

  Frame &str(const std::string &k, const std::string &v) {
    this->fields.push_back({k, true, v});
    return *this;
  }
  // Appends a float32 in its shortest form. NaN and the infinities have no wire form: the field is left out.
  Frame &num(const std::string &k, float v) {
    if (!std::isfinite(v)) return *this;
    char buf[64];
    auto r = std::to_chars(buf, buf + sizeof(buf), v);
    this->fields.push_back({k, false, std::string(buf, r.ptr)});
    return *this;
  }
  Frame &i64(const std::string &k, int64_t v) {
    char buf[32];
    auto r = std::to_chars(buf, buf + sizeof(buf), v);
    this->fields.push_back({k, false, std::string(buf, r.ptr)});
    return *this;
  }

  bool get_str(const std::string &k, std::string &out) const {
    const Field *f = this->find(k);
    if (f == nullptr || !f->is_str) return false;
    out = f->text;
    return true;
  }
  // The float32 that crossed the wire (strtof of the literal).
  bool get_num(const std::string &k, float &out) const {
    const Field *f = this->find(k);
    if (f == nullptr || f->is_str) return false;
    char *end = nullptr;
    float v = std::strtof(f->text.c_str(), &end);
    if (end == f->text.c_str() || *end != '\0' || !std::isfinite(v)) return false;
    out = v;
    return true;
  }
  // An integer: the text is -?[0-9]+ and fits in int64.
  bool get_int(const std::string &k, int64_t &out) const {
    const Field *f = this->find(k);
    if (f == nullptr || f->is_str || f->text.empty()) return false;
    const char *b = f->text.data();
    const char *e = b + f->text.size();
    for (const char *p = b; p < e; p++)
      if (!((*p >= '0' && *p <= '9') || (p == b && *p == '-' && e - b > 1))) return false;
    int64_t v = 0;
    auto r = std::from_chars(b, e, v);
    if (r.ec != std::errc() || r.ptr != e) return false;
    out = v;
    return true;
  }
};

enum class Err {
  NONE,
  EMPTY,
  TOO_LONG,
  NOT_OBJECT,
  NESTED,
  BAD_KEY,
  BAD_VALUE,
  BAD_ESCAPE,
  BAD_CHAR,
  DUP_KEY,
  MISSING_K,
  BAD_K,
  BAD_NUMBER,
  TRAILING,
};

// Lowercase class names, identical to the Go DecodeError.Class values and the invalid rows of frames.tsv.
inline const char *err_name(Err e) {
  switch (e) {
    case Err::NONE: return "none";
    case Err::EMPTY: return "empty";
    case Err::TOO_LONG: return "too_long";
    case Err::NOT_OBJECT: return "not_object";
    case Err::NESTED: return "nested";
    case Err::BAD_KEY: return "bad_key";
    case Err::BAD_VALUE: return "bad_value";
    case Err::BAD_ESCAPE: return "bad_escape";
    case Err::BAD_CHAR: return "bad_char";
    case Err::DUP_KEY: return "dup_key";
    case Err::MISSING_K: return "missing_k";
    case Err::BAD_K: return "bad_k";
    case Err::BAD_NUMBER: return "bad_number";
    case Err::TRAILING: return "trailing";
  }
  return "unknown";
}

namespace detail {

// -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?
inline bool valid_number(const std::string &t) {
  size_t i = 0;
  const size_t n = t.size();
  auto digits = [&]() {
    size_t start = i;
    while (i < n && t[i] >= '0' && t[i] <= '9') i++;
    return i - start;
  };
  if (i < n && t[i] == '-') i++;
  if (i < n && t[i] == '0') {
    i++;
  } else if (i < n && t[i] >= '1' && t[i] <= '9') {
    digits();
  } else {
    return false;
  }
  if (i < n && t[i] == '.') {
    i++;
    if (digits() == 0) return false;
  }
  if (i < n && (t[i] == 'e' || t[i] == 'E')) {
    i++;
    if (i < n && (t[i] == '+' || t[i] == '-')) i++;
    if (digits() == 0) return false;
  }
  return i == n;
}

inline bool is_number_byte(char c) {
  return (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E';
}

inline void append_quoted(std::string &out, const std::string &s) {
  out += '"';
  for (char ch : s) {
    unsigned char c = static_cast<unsigned char>(ch);
    if (c == '"' || c == '\\') {
      out += '\\';
      out += ch;
    } else if (c < 0x20 || c > 0x7e) {
      out += '?';  // outside printable ASCII: never emit a line the peer would reject
    } else {
      out += ch;
    }
  }
  out += '"';
}

inline Err parse_into(const std::string &line, Frame &out) {
  out.fields.clear();
  const size_t n = line.size();
  if (n == 0) return Err::EMPTY;
  if (n > MAX_LINE - 1) return Err::TOO_LONG;  // the newline counts toward MAX_LINE
  size_t i = 0;
  auto skip = [&]() {
    while (i < n && line[i] == ' ') i++;
  };
  auto end_of_object = [&]() -> Err {
    skip();
    return i == n ? Err::NONE : Err::TRAILING;
  };

  skip();
  if (i >= n || line[i] != '{') return Err::NOT_OBJECT;
  i++;
  skip();
  if (i < n && line[i] == '}') {
    i++;
    Err e = end_of_object();
    if (e != Err::NONE) return e;
    return Err::MISSING_K;  // "{}" has no k
  }

  for (;;) {
    skip();
    // key: "[a-z][a-z0-9_]*"
    if (i >= n) return Err::NOT_OBJECT;
    if (line[i] != '"') return Err::BAD_KEY;
    i++;
    const size_t kstart = i;
    while (i < n && line[i] != '"') {
      char c = line[i];
      bool first = i == kstart;
      bool lower = c >= 'a' && c <= 'z';
      bool rest = (c >= '0' && c <= '9') || c == '_';
      if (!lower && (first || !rest)) return Err::BAD_KEY;
      i++;
    }
    if (i >= n) return Err::NOT_OBJECT;
    if (i == kstart) return Err::BAD_KEY;
    std::string key = line.substr(kstart, i - kstart);
    i++;
    if (out.find(key) != nullptr) return Err::DUP_KEY;

    skip();
    if (i >= n || line[i] != ':') return Err::NOT_OBJECT;
    i++;
    skip();
    if (i >= n) return Err::NOT_OBJECT;

    // value
    char c = line[i];
    if (c == '"') {
      i++;
      std::string v;
      bool closed = false;
      while (i < n) {
        unsigned char ch = static_cast<unsigned char>(line[i]);
        if (ch == '"') {
          i++;
          closed = true;
          break;
        }
        if (ch == '\\') {
          if (i + 1 >= n || (line[i + 1] != '"' && line[i + 1] != '\\')) return Err::BAD_ESCAPE;
          v += line[i + 1];
          i += 2;
        } else if (ch < 0x20 || ch > 0x7e) {
          return Err::BAD_CHAR;
        } else {
          v += static_cast<char>(ch);
          i++;
        }
      }
      if (!closed) return Err::NOT_OBJECT;
      out.fields.push_back({key, true, v});
    } else if (c == '{' || c == '[') {
      return Err::NESTED;
    } else if (c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9')) {
      const size_t start = i;
      while (i < n && detail::is_number_byte(line[i])) i++;
      std::string tok = line.substr(start, i - start);
      if (tok.size() > MAX_NUMBER_LEN || !detail::valid_number(tok)) return Err::BAD_NUMBER;
      if (!std::isfinite(std::strtof(tok.c_str(), nullptr))) return Err::BAD_NUMBER;  // out of float32 range
      out.fields.push_back({key, false, tok});
    } else {
      return Err::BAD_VALUE;
    }

    skip();
    if (i >= n) return Err::NOT_OBJECT;
    if (line[i] == ',') {
      i++;
      continue;
    }
    if (line[i] != '}') return Err::NOT_OBJECT;
    i++;
    Err e = end_of_object();
    if (e != Err::NONE) return e;
    break;
  }

  const Field *k = out.find("k");
  if (k == nullptr) return Err::MISSING_K;
  if (!k->is_str || k->text.empty()) return Err::BAD_K;
  return Err::NONE;
}

}  // namespace detail

// Parses one line (without its newline) under the strict grammar. Single pass, no recursion; the
// length cap is checked first. Returns the first problem found, in the same order as the Go codec.
// On any error `out` is left empty.
inline Err parse(const std::string &line, Frame &out) {
  Err e = detail::parse_into(line, out);
  if (e != Err::NONE) out.fields.clear();
  return e;
}

// One line including the trailing newline: k as written first by callers, no spaces, strings escaped with \" and \\.
inline std::string encode(const Frame &f) {
  std::string out;
  out += '{';
  bool first = true;
  for (const auto &fd : f.fields) {
    if (!first) out += ',';
    first = false;
    detail::append_quoted(out, fd.key);
    out += ':';
    if (fd.is_str)
      detail::append_quoted(out, fd.text);
    else
      out += fd.text;
  }
  out += "}\n";
  return out;
}

}  // namespace esphome::sim_bus::proto
