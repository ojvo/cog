// Package util provides general-purpose utilities:
//   - Args: command-line argument parser with flags and subcommands
//   - Assert: lightweight test assertion helpers
//   - Caller: runtime caller inspection
//   - CSV: CSV reader/writer with header support
//   - DeepCopy: generic deep copy via JSON round-trip
//   - File: file system helpers (exists, copy, ensure dir)
//   - ID: unique ID generation (UUID, snowflake, sequential)
//   - Ignore: minimal .gitignore pattern matcher
//   - JSONL: streaming JSON-Lines reader (ForEachLine, SIMD-accelerated, O(1) memory),
//     append-only JSONL writer (JSONLWriter), and high-level JSONL reader
//     (JSONLReader, based on ForEachLine)
//   - Metrics: counter and gauge primitives
//   - Pagination: slice pagination helpers
//   - PathUtil: path manipulation helpers (SafeJoin, etc.)
//   - Sanitize: string sanitization for logs and output
//   - SecureToken: cryptographically secure random tokens
//   - Strings: UTF-8 safe string operations
//   - Truncation: output truncation with byte/line limits and head/tail mode
//   - Utils: miscellaneous helpers (min/max, contains, dedup)
//   - Validate: ID and string validation helpers
package util
