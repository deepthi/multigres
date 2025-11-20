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

package lsnutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLSN(t *testing.T) {
	tests := []struct {
		name      string
		lsn       string
		expected  uint64
		expectErr bool
	}{
		{
			name:      "zero LSN",
			lsn:       "0/0",
			expected:  0,
			expectErr: false,
		},
		{
			name:      "simple offset in first segment",
			lsn:       "0/3000000",
			expected:  0x3000000,
			expectErr: false,
		},
		{
			name:      "second segment",
			lsn:       "1/ABCD1234",
			expected:  (uint64(1) << 32) | 0xABCD1234,
			expectErr: false,
		},
		{
			name:      "high segment number",
			lsn:       "5/FFFF0000",
			expected:  (uint64(5) << 32) | 0xFFFF0000,
			expectErr: false,
		},
		{
			name:      "max uint32 values",
			lsn:       "FFFFFFFF/FFFFFFFF",
			expected:  0xFFFFFFFFFFFFFFFF,
			expectErr: false,
		},
		{
			name:      "lowercase hex",
			lsn:       "1/abcd1234",
			expected:  (uint64(1) << 32) | 0xabcd1234,
			expectErr: false,
		},
		{
			name:      "invalid format - no slash",
			lsn:       "invalid",
			expectErr: true,
		},
		{
			name:      "invalid format - too many parts",
			lsn:       "1/2/3",
			expectErr: true,
		},
		{
			name:      "invalid segment - not hex",
			lsn:       "X/1000000",
			expectErr: true,
		},
		{
			name:      "invalid offset - not hex",
			lsn:       "1/Y000000",
			expectErr: true,
		},
		{
			name:      "empty string",
			lsn:       "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseLSN(tt.lsn)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestCompareLSN(t *testing.T) {
	tests := []struct {
		name      string
		lsn1      string
		lsn2      string
		expected  int // -1, 0, or 1
		expectErr bool
	}{
		{
			name:     "equal LSNs",
			lsn1:     "0/3000000",
			lsn2:     "0/3000000",
			expected: 0,
		},
		{
			name:     "same segment, lsn1 < lsn2",
			lsn1:     "0/1000000",
			lsn2:     "0/3000000",
			expected: -1,
		},
		{
			name:     "same segment, lsn1 > lsn2",
			lsn1:     "0/3000000",
			lsn2:     "0/1000000",
			expected: 1,
		},
		{
			name:     "different segments, lsn1 < lsn2",
			lsn1:     "0/9000000",
			lsn2:     "1/1000000",
			expected: -1,
		},
		{
			name:     "different segments, lsn1 > lsn2",
			lsn1:     "5/1000000",
			lsn2:     "1/9000000",
			expected: 1,
		},
		{
			name:     "zero vs non-zero",
			lsn1:     "0/0",
			lsn2:     "0/1",
			expected: -1,
		},
		{
			name:     "max values equal",
			lsn1:     "FFFFFFFF/FFFFFFFF",
			lsn2:     "FFFFFFFF/FFFFFFFF",
			expected: 0,
		},
		{
			name:      "invalid lsn1",
			lsn1:      "invalid",
			lsn2:      "0/1000000",
			expectErr: true,
		},
		{
			name:      "invalid lsn2",
			lsn1:      "0/1000000",
			lsn2:      "invalid",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CompareLSN(tt.lsn1, tt.lsn2)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestLSNGreater(t *testing.T) {
	tests := []struct {
		name      string
		lsn1      string
		lsn2      string
		expected  bool
		expectErr bool
	}{
		{
			name:     "lsn1 > lsn2 same segment",
			lsn1:     "0/3000000",
			lsn2:     "0/1000000",
			expected: true,
		},
		{
			name:     "lsn1 > lsn2 different segments",
			lsn1:     "1/1000000",
			lsn2:     "0/9000000",
			expected: true,
		},
		{
			name:     "lsn1 < lsn2",
			lsn1:     "0/1000000",
			lsn2:     "0/3000000",
			expected: false,
		},
		{
			name:     "lsn1 == lsn2",
			lsn1:     "0/3000000",
			lsn2:     "0/3000000",
			expected: false,
		},
		{
			name:      "invalid lsn1",
			lsn1:      "invalid",
			lsn2:      "0/1000000",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LSNGreater(tt.lsn1, tt.lsn2)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestLSNGreaterOrEqual(t *testing.T) {
	tests := []struct {
		name      string
		lsn1      string
		lsn2      string
		expected  bool
		expectErr bool
	}{
		{
			name:     "lsn1 > lsn2",
			lsn1:     "0/3000000",
			lsn2:     "0/1000000",
			expected: true,
		},
		{
			name:     "lsn1 == lsn2",
			lsn1:     "0/3000000",
			lsn2:     "0/3000000",
			expected: true,
		},
		{
			name:     "lsn1 < lsn2",
			lsn1:     "0/1000000",
			lsn2:     "0/3000000",
			expected: false,
		},
		{
			name:      "invalid lsn2",
			lsn1:      "0/1000000",
			lsn2:      "invalid",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := LSNGreaterOrEqual(tt.lsn1, tt.lsn2)

			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
