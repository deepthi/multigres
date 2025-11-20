// Copyright 2025 Supabase, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package lsnutil provides utilities for working with PostgreSQL LSN (Log Sequence Number) values.
package lsnutil

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseLSN parses a PostgreSQL LSN string (format: X/XXXXXXXX) into a uint64.
//
// PostgreSQL LSNs are 64-bit values consisting of:
// - Upper 32 bits: segment/file number (before the slash)
// - Lower 32 bits: offset within segment (after the slash)
//
// Both parts are hexadecimal strings.
//
// Examples:
//   - "0/0" -> 0
//   - "0/3000000" -> 0x3000000
//   - "1/ABCD1234" -> 0x1ABCD1234
func ParseLSN(lsn string) (uint64, error) {
	if lsn == "" {
		return 0, fmt.Errorf("empty LSN string")
	}

	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid LSN format (expected X/XXXXXXXX): %s", lsn)
	}

	// Parse upper 32 bits (segment number)
	segment, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN segment (expected hex): %s", parts[0])
	}

	// Parse lower 32 bits (offset within segment)
	offset, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN offset (expected hex): %s", parts[1])
	}

	// Combine into 64-bit value: (segment << 32) | offset
	return (segment << 32) | offset, nil
}

// CompareLSN compares two PostgreSQL LSN strings.
//
// Returns:
//   - -1 if lsn1 < lsn2
//   - 0 if lsn1 == lsn2
//   - 1 if lsn1 > lsn2
//   - error if either LSN string is invalid
func CompareLSN(lsn1, lsn2 string) (int, error) {
	val1, err := ParseLSN(lsn1)
	if err != nil {
		return 0, fmt.Errorf("invalid lsn1: %w", err)
	}

	val2, err := ParseLSN(lsn2)
	if err != nil {
		return 0, fmt.Errorf("invalid lsn2: %w", err)
	}

	if val1 < val2 {
		return -1, nil
	} else if val1 > val2 {
		return 1, nil
	}
	return 0, nil
}

// LSNGreater returns true if lsn1 > lsn2.
// Returns error if either LSN string is invalid.
func LSNGreater(lsn1, lsn2 string) (bool, error) {
	cmp, err := CompareLSN(lsn1, lsn2)
	if err != nil {
		return false, err
	}
	return cmp > 0, nil
}

// LSNGreaterOrEqual returns true if lsn1 >= lsn2.
// Returns error if either LSN string is invalid.
func LSNGreaterOrEqual(lsn1, lsn2 string) (bool, error) {
	cmp, err := CompareLSN(lsn1, lsn2)
	if err != nil {
		return false, err
	}
	return cmp >= 0, nil
}
