package types

import (
	"encoding/json"
	"testing"

	qt "github.com/frankban/quicktest"
)

func TestHexBytes(t *testing.T) {
	c := qt.New(t)

	c.Run("Bytes", func(c *qt.C) {
		hb := HexBytes{0x01, 0x02, 0x03}
		out := (&hb).Bytes()
		c.Assert(out, qt.DeepEquals, []byte{0x01, 0x02, 0x03})

		out[0] = 0xFF
		c.Assert(hb[0], qt.Equals, byte(0xFF))
	})

	c.Run("String", func(c *qt.C) {
		testCases := []struct {
			name string
			in   HexBytes
			want string
		}{
			{name: "nil slice", in: nil, want: "0x"},
			{name: "empty", in: HexBytes{}, want: "0x"},
			{name: "non-empty", in: HexBytes{0x00, 0xAB, 0xCD}, want: "0x00abcd"},
		}

		for _, tc := range testCases {
			tc := tc
			c.Run(tc.name, func(c *qt.C) {
				c.Assert((&tc.in).String(), qt.Equals, tc.want)
			})
		}
	})

	c.Run("BigInt", func(c *qt.C) {
		testCases := []struct {
			name string
			in   HexBytes
			want string
		}{
			{name: "empty", in: HexBytes{}, want: "0"},
			{name: "big-endian", in: HexBytes{0x01, 0x00}, want: "256"},
			{name: "leading zeros", in: HexBytes{0x00, 0x00, 0x02}, want: "2"},
		}

		for _, tc := range testCases {
			tc := tc
			c.Run(tc.name, func(c *qt.C) {
				c.Assert((&tc.in).BigInt().String(), qt.Equals, tc.want)
			})
		}
	})

	c.Run("LeftPad", func(c *qt.C) {
		testCases := []struct {
			name string
			in   HexBytes
			n    int
			want HexBytes
		}{
			{
				name: "shorter pads with zeros",
				in:   HexBytes{0xAA, 0xBB},
				n:    4,
				want: HexBytes{0x00, 0x00, 0xAA, 0xBB},
			},
			{
				name: "equal length copy",
				in:   HexBytes{0xAA, 0xBB},
				n:    2,
				want: HexBytes{0xAA, 0xBB},
			},
			{
				name: "longer returns copy",
				in:   HexBytes{0xAA, 0xBB},
				n:    1,
				want: HexBytes{0xAA, 0xBB},
			},
			{
				name: "pad to zero length",
				in:   HexBytes{},
				n:    0,
				want: HexBytes{},
			},
		}

		for _, tc := range testCases {
			tc := tc
			c.Run(tc.name, func(c *qt.C) {
				out := tc.in.LeftPad(tc.n)
				c.Assert(out, qt.DeepEquals, tc.want)

				if len(out) > 0 {
					originalFirst := byte(0x00)
					if len(tc.in) > 0 {
						originalFirst = tc.in[0]
					}
					out[0] ^= 0xFF
					if len(tc.in) > 0 {
						c.Assert(tc.in[0], qt.Equals, originalFirst)
					}
				}
			})
		}
	})

	c.Run("Hex32Bytes", func(c *qt.C) {
		in := HexBytes{0x01, 0x02}
		out := in.Hex32Bytes()
		c.Assert(len(out), qt.Equals, 32)
		c.Assert(out[30:], qt.DeepEquals, HexBytes{0x01, 0x02})
		c.Assert(out[:30], qt.DeepEquals, HexBytes(make([]byte, 30)))

		out[31] = 0xFF
		c.Assert(in[1], qt.Equals, byte(0x02))
	})

	c.Run("LeftTrim", func(c *qt.C) {
		testCases := []struct {
			name string
			in   HexBytes
			want HexBytes
		}{
			{
				name: "no leading zeros returns copy",
				in:   HexBytes{0x01, 0x02},
				want: HexBytes{0x01, 0x02},
			},
			{
				name: "trims leading zeros",
				in:   HexBytes{0x00, 0x00, 0x01, 0x02},
				want: HexBytes{0x01, 0x02},
			},
			{
				name: "all zeros becomes empty",
				in:   HexBytes{0x00, 0x00},
				want: HexBytes{},
			},
			{
				name: "empty stays empty",
				in:   HexBytes{},
				want: HexBytes{},
			},
		}

		for _, tc := range testCases {
			tc := tc
			c.Run(tc.name, func(c *qt.C) {
				out := tc.in.LeftTrim()
				c.Assert(out, qt.DeepEquals, tc.want)

				if len(tc.in) > 0 {
					out2 := tc.in.LeftTrim()
					if len(out2) > 0 {
						original := tc.in[0]
						out2[0] ^= 0xFF
						c.Assert(tc.in[0], qt.Equals, original)
					}
				}
			})
		}
	})

	c.Run("JSON", func(c *qt.C) {
		c.Run("MarshalJSON", func(c *qt.C) {
			testCases := []struct {
				name string
				in   HexBytes
				want string
			}{
				{name: "empty", in: HexBytes{}, want: `"0x"`},
				{name: "non-empty", in: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}, want: `"0xdeadbeef"`},
			}

			for _, tc := range testCases {
				tc := tc
				c.Run(tc.name, func(c *qt.C) {
					b, err := tc.in.MarshalJSON()
					c.Assert(err, qt.IsNil)
					c.Assert(string(b), qt.Equals, tc.want)

					viaJSON, err := json.Marshal(tc.in)
					c.Assert(err, qt.IsNil)
					c.Assert(string(viaJSON), qt.Equals, tc.want)
				})
			}
		})

		c.Run("UnmarshalJSON valid", func(c *qt.C) {
			testCases := []struct {
				name string
				in   string
				want HexBytes
			}{
				{name: "with 0x prefix", in: `"0xdeadbeef"`, want: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}},
				{name: "with 0X prefix", in: `"0Xdeadbeef"`, want: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}},
				{name: "without prefix", in: `"deadbeef"`, want: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}},
				{name: "empty", in: `"0x"`, want: HexBytes{}},
			}

			for _, tc := range testCases {
				tc := tc
				c.Run(tc.name, func(c *qt.C) {
					var hb HexBytes
					c.Assert(json.Unmarshal([]byte(tc.in), &hb), qt.IsNil)
					if len(tc.want) == 0 {
						c.Assert(len(hb), qt.Equals, 0)
						return
					}
					c.Assert(hb, qt.DeepEquals, tc.want)
				})
			}
		})

		c.Run("UnmarshalJSON invalid", func(c *qt.C) {
			testCases := []struct {
				name string
				in   string
				re   string
			}{
				{name: "not a JSON string", in: `123`, re: `invalid JSON string: "123"`},
				{name: "odd length", in: `"0x0"`, re: `encoding/hex: odd length hex string`},
				{name: "invalid byte", in: `"0xzz"`, re: `encoding/hex: invalid byte: .*`},
			}

			for _, tc := range testCases {
				tc := tc
				c.Run(tc.name, func(c *qt.C) {
					var hb HexBytes
					c.Assert(json.Unmarshal([]byte(tc.in), &hb), qt.ErrorMatches, tc.re)
				})
			}
		})

		c.Run("UnmarshalJSON invalid raw bytes", func(c *qt.C) {
			var hb HexBytes
			c.Assert(hb.UnmarshalJSON([]byte(`"0x00`)), qt.ErrorMatches, `invalid JSON string: .*`)
		})

		c.Run("UnmarshalJSON reslices to decoded length", func(c *qt.C) {
			hb := HexBytes{0xAA, 0xBB, 0xCC, 0xDD}
			c.Assert(json.Unmarshal([]byte(`"0x01"`), &hb), qt.IsNil)
			c.Assert(hb, qt.DeepEquals, HexBytes{0x01})
			c.Assert(len(hb), qt.Equals, 1)
		})
	})

	c.Run("HexStringToHexBytes", func(c *qt.C) {
		testCases := []struct {
			name string
			in   string
			want HexBytes
		}{
			{name: "with prefix", in: "0xdeadbeef", want: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}},
			{name: "with uppercase prefix", in: "0Xdeadbeef", want: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}},
			{name: "without prefix", in: "deadbeef", want: HexBytes{0xDE, 0xAD, 0xBE, 0xEF}},
			{name: "empty", in: "", want: HexBytes{}},
		}

		for _, tc := range testCases {
			tc := tc
			c.Run(tc.name, func(c *qt.C) {
				got, err := HexStringToHexBytes(tc.in)
				c.Assert(err, qt.IsNil)
				c.Assert(got, qt.DeepEquals, tc.want)
			})
		}

		_, err := HexStringToHexBytes("0xzz")
		c.Assert(err, qt.ErrorMatches, `invalid hex string "zz": .*`)
	})

	c.Run("HexStringToHexBytesMustUnmarshal", func(c *qt.C) {
		c.Run("valid", func(c *qt.C) {
			c.Assert(HexStringToHexBytesMustUnmarshal("0xdeadbeef"), qt.DeepEquals, HexBytes{0xDE, 0xAD, 0xBE, 0xEF})
		})

		c.Run("panic on invalid", func(c *qt.C) {
			c.Assert(func() {
				_ = HexStringToHexBytesMustUnmarshal("0xzz")
			}, qt.PanicMatches, `encoding/hex: invalid byte: .*`)
		})
	})
}
