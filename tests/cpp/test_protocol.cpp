// Golden-vector test for the header-only sim-bus codec (BUS-04).
//
// Reads protocol/vectors/frames.tsv (path in argv[1]), the same file the Go test
// internal/bus/codec_test.go reads, and checks every row against
// components/sim_bus/sim_protocol.h. Standard C++20 only. This file must never
// live under components/: ESPHome compiles every .cpp it finds there.
//
// Build and run: make test-cpp (host g++), make test-cpp-image (image gcc).
#include "sim_protocol.h"

#include <cerrno>
#include <charconv>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <limits>
#include <map>
#include <string>
#include <vector>

namespace proto = esphome::sim_bus::proto;

namespace {

int g_passed = 0;
int g_failed = 0;

void fail(const std::string &name, const std::string &why) {
  g_failed++;
  std::printf("FAIL %s: %s\n", name.c_str(), why.c_str());
}
void pass() { g_passed++; }

std::vector<std::string> split(const std::string &s, char sep) {
  std::vector<std::string> out;
  size_t start = 0;
  for (;;) {
    size_t p = s.find(sep, start);
    if (p == std::string::npos) {
      out.push_back(s.substr(start));
      return out;
    }
    out.push_back(s.substr(start, p - start));
    start = p + 1;
  }
}

// %HH -> byte. Returns false on a malformed escape.
bool unescape_pct(const std::string &in, std::string &out) {
  out.clear();
  for (size_t i = 0; i < in.size(); i++) {
    if (in[i] != '%') {
      out += in[i];
      continue;
    }
    if (i + 2 >= in.size()) return false;
    unsigned v = 0;
    auto r = std::from_chars(in.data() + i + 1, in.data() + i + 3, v, 16);
    if (r.ec != std::errc() || r.ptr != in.data() + i + 3) return false;
    out += static_cast<char>(v);
    i += 2;
  }
  return true;
}

struct TupleField {
  std::string key;
  char type;  // s, n, i
  std::string val;
};

bool parse_tuple(const std::string &tuple, std::vector<TupleField> &out) {
  out.clear();
  for (const auto &part : split(tuple, ';')) {
    size_t eq = part.find('=');
    if (eq == std::string::npos || eq == 0 || part.size() < eq + 3 || part[eq + 2] != ':') return false;
    out.push_back({part.substr(0, eq), part[eq + 1], part.substr(eq + 3)});
  }
  return true;
}

bool to_i64(const std::string &s, int64_t &v) {
  auto r = std::from_chars(s.data(), s.data() + s.size(), v);
  return r.ec == std::errc() && r.ptr == s.data() + s.size();
}

float to_f32(const std::string &s) { return std::strtof(s.c_str(), nullptr); }

// Frame built through the public builders, in tuple order.
bool frame_from_tuple(const std::string &tuple, proto::Frame &f, std::string &why) {
  std::vector<TupleField> tf;
  if (!parse_tuple(tuple, tf)) {
    why = "bad tuple '" + tuple + "'";
    return false;
  }
  f.fields.clear();
  for (const auto &t : tf) {
    if (t.type == 's') {
      f.str(t.key, t.val);
    } else if (t.type == 'n') {
      f.num(t.key, to_f32(t.val));
    } else if (t.type == 'i') {
      int64_t v = 0;
      if (!to_i64(t.val, v)) {
        why = "bad tuple int '" + t.val + "'";
        return false;
      }
      f.i64(t.key, v);
    } else {
      why = "bad tuple type";
      return false;
    }
  }
  return true;
}

// Compares a decoded frame with a tuple by value and in order. Empty string on success.
std::string check_tuple(const proto::Frame &f, const std::string &tuple) {
  std::vector<TupleField> tf;
  if (!parse_tuple(tuple, tf)) return "bad tuple '" + tuple + "'";
  if (f.fields.size() != tf.size())
    return "got " + std::to_string(f.fields.size()) + " fields, want " + std::to_string(tf.size());
  for (size_t i = 0; i < tf.size(); i++) {
    const auto &want = tf[i];
    const auto &got = f.fields[i];
    if (got.key != want.key) return "field " + std::to_string(i) + ": key '" + got.key + "', want '" + want.key + "'";
    if (want.type == 's') {
      std::string s;
      if (!f.get_str(want.key, s) || s != want.val) return want.key + ": string '" + s + "', want '" + want.val + "'";
    } else if (want.type == 'n') {
      float v = 0;
      if (got.is_str || !f.get_num(want.key, v)) return want.key + ": not a number";
      // Same rounding on both texts: the decoded literal and the expected literal.
      if (v != to_f32(want.val) || to_f32(got.text) != to_f32(want.val))
        return want.key + ": value " + got.text + " != " + want.val + " as float32";
    } else if (want.type == 'i') {
      int64_t v = 0, w = 0;
      if (got.is_str || !f.get_int(want.key, v) || !to_i64(want.val, w) || v != w)
        return want.key + ": int '" + got.text + "', want " + want.val;
    }
  }
  return "";
}

std::string strip_newline(const std::string &s) { return (!s.empty() && s.back() == '\n') ? s.substr(0, s.size() - 1) : s; }

struct Row {
  std::string name, kind, line, expect;
};

}  // namespace

int main(int argc, char **argv) {
  if (argc < 2) {
    std::fprintf(stderr, "usage: %s protocol/vectors/frames.tsv\n", argv[0]);
    return 2;
  }
  std::ifstream in(argv[1]);
  if (!in) {
    std::fprintf(stderr, "cannot open %s\n", argv[1]);
    return 2;
  }

  std::vector<Row> rows;
  std::map<std::string, bool> seen;
  std::string raw;
  int lineno = 0;
  while (std::getline(in, raw)) {
    lineno++;
    if (raw.empty() || raw[0] == '#') continue;
    auto cols = split(raw, '\t');
    if (cols.size() != 4) {
      fail("line " + std::to_string(lineno), "want 4 tab-separated columns, got " + std::to_string(cols.size()));
      continue;
    }
    Row r{cols[0], cols[1], "", cols[3]};
    if (!unescape_pct(cols[2], r.line)) {
      fail(r.name, "bad %-escape in line column");
      continue;
    }
    if (seen[r.name]) {
      fail(r.name, "duplicate row name");
      continue;
    }
    seen[r.name] = true;
    if (r.kind != "valid" && r.kind != "invalid" && r.kind != "encode") {
      fail(r.name, "unknown row kind '" + r.kind + "'");
      continue;
    }
    rows.push_back(r);
  }
  if (rows.empty()) {
    std::fprintf(stderr, "no vector rows in %s\n", argv[1]);
    return 2;
  }

  for (const auto &r : rows) {
    if (r.kind == "valid") {
      proto::Frame f;
      proto::Err e = proto::parse(r.line, f);
      if (e != proto::Err::NONE) {
        fail(r.name, std::string("parse returned ") + proto::err_name(e));
        continue;
      }
      std::string why = check_tuple(f, r.expect);
      if (!why.empty()) {
        fail(r.name, why);
        continue;
      }
      // Round trip: encode(frame built from the tuple) parses back to the same tuple.
      proto::Frame b;
      if (!frame_from_tuple(r.expect, b, why)) {
        fail(r.name, why);
        continue;
      }
      std::string enc = proto::encode(b);
      proto::Frame back;
      e = proto::parse(strip_newline(enc), back);
      if (e != proto::Err::NONE) {
        fail(r.name, std::string("round trip: parse returned ") + proto::err_name(e) + " for " + enc);
        continue;
      }
      why = check_tuple(back, r.expect);
      if (!why.empty()) {
        fail(r.name, "round trip: " + why);
        continue;
      }
      pass();
    } else if (r.kind == "invalid") {
      proto::Frame f;
      proto::Err e = proto::parse(r.line, f);
      if (e == proto::Err::NONE) {
        fail(r.name, "parse accepted the line, want " + r.expect);
      } else if (r.expect != proto::err_name(e)) {
        fail(r.name, std::string("class ") + proto::err_name(e) + ", want " + r.expect);
      } else {
        pass();
      }
    } else {
      proto::Frame f;
      std::string why;
      if (!frame_from_tuple(r.expect, f, why)) {
        fail(r.name, why);
        continue;
      }
      std::string got = proto::encode(f);
      if (got != r.line + "\n") {
        fail(r.name, "encode = '" + got + "', want '" + r.line + "\\n'");
      } else {
        pass();
      }
    }
  }

  // Properties that do not fit a TSV row.
  {
    proto::Frame f;
    f.str("k", "out");
    f.num("v", std::numeric_limits<float>::quiet_NaN());
    f.num("v", std::numeric_limits<float>::infinity());
    f.num("v", -std::numeric_limits<float>::infinity());
    if (f.fields.size() != 1) {
      fail("num_non_finite", "num() added a field for NaN/Inf");
    } else if (proto::encode(f) != "{\"k\":\"out\"}\n") {
      fail("num_non_finite", "encode = " + proto::encode(f));
    } else {
      pass();
    }
  }
  {
    proto::Frame f;
    f.str("k", "err").str("msg", std::string("a\tb\xc3\xa9\x7f\n"));
    std::string line = proto::encode(f);
    proto::Frame back;
    if (line != "{\"k\":\"err\",\"msg\":\"a?b????\"}\n") {
      fail("encode_sanitizes", "encode = " + line);
    } else if (proto::parse(strip_newline(line), back) != proto::Err::NONE) {
      fail("encode_sanitizes", "sanitized line does not parse");
    } else {
      pass();
    }
  }
  {
    // get_int is strict: no fraction, exponent or string; int64 range.
    proto::Frame f;
    if (proto::parse("{\"k\":\"x\",\"a\":1.5,\"b\":1e3,\"c\":9223372036854775808,\"d\":-7,\"e\":\"5\"}", f) !=
        proto::Err::NONE) {
      fail("get_int_strict", "setup line does not parse");
    } else {
      int64_t v = 0;
      bool bad = f.get_int("a", v) || f.get_int("b", v) || f.get_int("c", v) || f.get_int("e", v);
      if (bad || !f.get_int("d", v) || v != -7)
        fail("get_int_strict", "get_int accepted a non-integer or rejected -7");
      else
        pass();
    }
  }
  {
    // Every prefix and every single-byte deletion of every vector line parses or returns an Err; no crash.
    int checked = 0;
    for (const auto &r : rows) {
      if (r.kind == "encode") continue;
      for (size_t i = 0; i <= r.line.size(); i++) {
        proto::Frame f;
        (void) proto::parse(r.line.substr(0, i), f);
        if (i < r.line.size()) {
          std::string cut = r.line.substr(0, i) + r.line.substr(i + 1);
          (void) proto::parse(cut, f);
        }
        checked++;
      }
    }
    if (checked == 0)
      fail("never_crashes", "nothing checked");
    else
      pass();
  }

  std::printf("%d passed, %d failed\n", g_passed, g_failed);
  return g_failed == 0 ? 0 : 1;
}
