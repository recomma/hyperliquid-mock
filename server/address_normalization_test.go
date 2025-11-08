package server

import (
	"testing"
)

// TestNormalizeAddress verifies address normalization for case-insensitive comparison
func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name     string
		addr1    string
		addr2    string
		expected bool // should they be equal after normalization?
	}{
		{
			name:     "identical lowercase addresses",
			addr1:    "0xeb5df7323c643f01b8c0643be808a0e6486621e8",
			addr2:    "0xeb5df7323c643f01b8c0643be808a0e6486621e8",
			expected: true,
		},
		{
			name:     "identical uppercase addresses",
			addr1:    "0XEB5DF7323C643F01B8C0643BE808A0E6486621E8",
			addr2:    "0XEB5DF7323C643F01B8C0643BE808A0E6486621E8",
			expected: true,
		},
		{
			name:     "mixed case vs lowercase (EIP-55 checksum)",
			addr1:    "0xeb5Df7323c643f01b8C0643bE808a0e6486621e8", // checksummed
			addr2:    "0xeb5df7323c643f01b8c0643be808a0e6486621e8", // lowercase
			expected: true,
		},
		{
			name:     "mixed case vs uppercase",
			addr1:    "0xeb5Df7323c643f01b8C0643bE808a0e6486621e8", // checksummed
			addr2:    "0XEB5DF7323C643F01B8C0643BE808A0E6486621E8", // uppercase
			expected: true,
		},
		{
			name:     "different addresses (lowercase)",
			addr1:    "0xeb5df7323c643f01b8c0643be808a0e6486621e8",
			addr2:    "0x628f0be408bdf24451a1c30c452abbb9cfb50a18",
			expected: false,
		},
		{
			name:     "different addresses (mixed case)",
			addr1:    "0xeb5Df7323c643f01b8C0643bE808a0e6486621e8",
			addr2:    "0x628f0bE408bdf24451a1c30C452abbB9cfb50A18",
			expected: false,
		},
		{
			name:     "empty addresses",
			addr1:    "",
			addr2:    "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			norm1 := normalizeAddress(tt.addr1)
			norm2 := normalizeAddress(tt.addr2)
			equal := norm1 == norm2

			if equal != tt.expected {
				t.Errorf("normalizeAddress(%q) vs normalizeAddress(%q):\ngot equal=%v, want equal=%v\nnorm1=%q, norm2=%q",
					tt.addr1, tt.addr2, equal, tt.expected, norm1, norm2)
			}
		})
	}
}
