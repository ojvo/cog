// Package util provides general-purpose utilities:
//   - Args: command-line argument parser with flags and subcommands
//   - Assert: lightweight test assertion helpers
//   - Caller: runtime caller inspection
//   - Conv: primitive/dynamic type conversion helpers (AsString/AsInt64/AsBool/AsAnySlice/CopyValue)
//   - DeepCopy: generic deep copy via JSON round-trip
//   - ID: unique ID generation (UUID, snowflake, sequential)
//   - Ignore: minimal .gitignore pattern matcher
//   - Metrics: counter and gauge primitives
//   - Pagination: slice pagination helpers
//   - Random: non-cryptographic random string/byte/int helpers (math/rand/v2 backed)
//   - Sanitize: string sanitization for logs and output
//   - SecureToken: cryptographically secure random tokens
//   - Strings: UTF-8 safe string operations
//   - Unicode: Unicode escape encode/decode helpers (Unicode, ToUnicode, ToUC, UnicodeToUTF8)
//   - Truncation: output truncation with byte/line limits and head/tail mode
//   - Utils: miscellaneous helpers (min/max, contains, dedup)
//   - Validate: ID and string validation helpers
package util
