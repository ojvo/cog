// Package dafe (data-format engine) provides structured file I/O for common
// persistence formats, organized by format family into sub-packages:
//
//   - dafe/csv    CSV reader/writer with three abstraction layers:
//     file-level Open/CSV/Writer[T], generic typed Reader[T] (struct tags,
//     CsvMarshal/CustomSetter interfaces, snake_case fallback, WithCheck),
//     and concurrent CSVWriter/CSVReader with atomic UpdateRow. Also
//     provides pipe-separated Record/Records map helpers and Unmarshal
//     (Record → struct with datefmt tag).
//   - dafe/json   JSON and JSON Lines: LoadJSONFile/SaveJSONFile,
//     ForEachLine (SIMD-accelerated streaming), JSONLWriter/JSONLReader,
//     ExtractJSON/ExtractJSONArray (LLM response extraction with markdown
//     fence and balanced-brace scanning)
//
// The root package provides format-agnostic conversion:
//
//   - ConvertInto   tag-aware struct/map/slice conversion (defaults to "json" tag)
//   - AsAnyMap / AsStringMap   value-to-map projection
//   - ConvertOption   tag-selection control
//
// Choose by need: dafe/csv for CSV; dafe/json for JSON/JSONL;
// ConvertInto/AsAnyMap for cross-format projection.
package dafe
