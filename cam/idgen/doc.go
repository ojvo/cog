// Package idgen provides distributed-friendly unique ID generators.
//
// Two algorithms are offered, each with different trade-offs:
//
//   - Snowflake: 63-bit IDs (41b ms timestamp / 10b machineID / 12b sequence).
//     Strictly time-ordered within a single generator; safe across machines
//     when each node has a distinct machineID. Blocks (spin-yields) when the
//     sequence overflows in the same millisecond.
//   - Mist: 63-bit IDs (47b monotonic counter / 8b salt A / 8b salt B).
//     Globally unique without coordination; high-order counter preserves
//     rough monotonicity while the 16-bit random salt makes IDs unpredictable.
//     Uses crypto/rand for the salt — at the cost of one crypto RNG draw per
//     ID. Suitable when predictability of successive IDs is undesirable.
//
// Both generators are safe for concurrent use.
package idgen
