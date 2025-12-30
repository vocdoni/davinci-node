package types

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/vocdoni/davinci-node/types/params"
)

// BallotMode is the struct to define the rules of a ballot
type BallotMode struct {
	NumFields      uint8   `json:"numFields" cbor:"0,keyasint,omitempty"`
	UniqueValues   bool    `json:"uniqueValues" cbor:"1,keyasint,omitempty"`
	MaxValue       *BigInt `json:"maxValue" cbor:"2,keyasint,omitempty"`
	MinValue       *BigInt `json:"minValue" cbor:"3,keyasint,omitempty"`
	MaxValueSum    *BigInt `json:"maxValueSum" cbor:"4,keyasint,omitempty"`
	MinValueSum    *BigInt `json:"minValueSum" cbor:"5,keyasint,omitempty"`
	CostExponent   uint8   `json:"costExponent" cbor:"6,keyasint,omitempty"`
	CostFromWeight bool    `json:"costFromWeight" cbor:"7,keyasint,omitempty"`
}

func (b *BallotMode) Validate() error {
	// Validate NumFields
	if int(b.NumFields) > params.FieldsPerBallot {
		return fmt.Errorf("numFields %d is greater than max size %d", b.NumFields, params.FieldsPerBallot)
	}

	// Validate MaxValueSum
	if b.MaxValueSum == nil {
		return fmt.Errorf("maxValueSum is nil")
	}

	// Validate MinValueSum
	if b.MinValueSum == nil {
		return fmt.Errorf("minValueSum is nil")
	}

	// Ensure MinValue is not greater than MaxValue
	if b.MinValue.MathBigInt().Cmp(b.MaxValue.MathBigInt()) > 0 {
		return fmt.Errorf("minValue %s is greater than maxValue %s", b.MinValue.String(), b.MaxValue.String())
	}

	// Ensure MinValueSum is not greater than MaxValueSum
	if b.MinValueSum.MathBigInt().Cmp(b.MaxValueSum.MathBigInt()) > 0 {
		return fmt.Errorf("minValueSum %s is greater than maxValueSum %s", b.MinValueSum.String(), b.MaxValueSum.String())
	}

	return nil
}

// writeBigInt serializes a types.BigInt into the buffer as length + bytes
func writeBigInt(buf *bytes.Buffer, bi *BigInt) error {
	data := bi.Bytes()
	length := uint32(len(data))
	err := binary.Write(buf, binary.BigEndian, length)
	if err != nil {
		return fmt.Errorf("failed to write big int length: %v", err)
	}
	if length > 0 {
		_, err = buf.Write(data)
		if err != nil {
			return fmt.Errorf("failed to write big int data: %v", err)
		}
	}
	return nil
}

// readBigInt deserializes a types.BigInt from the buffer
func readBigInt(buf *bytes.Reader, bi *BigInt) error {
	if bi == nil {
		return fmt.Errorf("big int is nil")
	}
	var length uint32
	err := binary.Read(buf, binary.BigEndian, &length)
	if err != nil {
		return fmt.Errorf("failed to read big int length: %v", err)
	}
	data := make([]byte, length)
	if length > 0 {
		_, err = buf.Read(data)
		if err != nil {
			return fmt.Errorf("failed to read big int data: %v", err)
		}
	}
	bi.SetBytes(data)
	return nil
}

// Marshal serializes the BallotMode into a byte slice
func (b *BallotMode) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)

	// NumFields (1 byte)
	err := buf.WriteByte(b.NumFields)
	if err != nil {
		return nil, fmt.Errorf("failed to write NumFields: %v", err)
	}

	// UniqueValues (1 byte: 0 or 1)
	force := byte(0)
	if b.UniqueValues {
		force = 1
	}
	err = buf.WriteByte(force)
	if err != nil {
		return nil, fmt.Errorf("failed to write UniqueValues: %v", err)
	}

	// MaxValue
	if err := writeBigInt(buf, b.MaxValue); err != nil {
		return nil, err
	}

	// MinValue
	if err := writeBigInt(buf, b.MinValue); err != nil {
		return nil, err
	}

	// MaxValueSum
	if err := writeBigInt(buf, b.MaxValueSum); err != nil {
		return nil, err
	}

	// MinValueSum
	if err := writeBigInt(buf, b.MinValueSum); err != nil {
		return nil, err
	}

	// CostExponent (1 byte)
	err = buf.WriteByte(b.CostExponent)
	if err != nil {
		return nil, fmt.Errorf("failed to write CostExponent: %v", err)
	}

	// CostFromWeight (1 byte: 0 or 1)
	costW := byte(0)
	if b.CostFromWeight {
		costW = 1
	}
	err = buf.WriteByte(costW)
	if err != nil {
		return nil, fmt.Errorf("failed to write CostFromWeight: %v", err)
	}

	return buf.Bytes(), nil
}

// Unmarshal deserializes the BallotMode from a byte slice
func (b *BallotMode) Unmarshal(data []byte) error {
	buf := bytes.NewReader(data)

	// NumFields
	numFields, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read NumFields: %v", err)
	}
	b.NumFields = numFields

	// UniqueValues
	force, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read UniqueValues: %v", err)
	}
	b.UniqueValues = (force == 1)

	// MaxValue
	if err := readBigInt(buf, b.MaxValue); err != nil {
		return err
	}

	// MinValue
	if err := readBigInt(buf, b.MinValue); err != nil {
		return err
	}

	// MaxValueSum
	if err := readBigInt(buf, b.MaxValueSum); err != nil {
		return err
	}

	// MinValueSum
	if err := readBigInt(buf, b.MinValueSum); err != nil {
		return err
	}

	// CostExponent
	costExponent, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read CostExponent: %v", err)
	}
	b.CostExponent = costExponent

	// CostFromWeight
	costW, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read CostFromWeight: %v", err)
	}
	b.CostFromWeight = (costW == 1)

	return nil
}

// MarshalJSON implements json.Marshaler interface
func (b *BallotMode) MarshalJSON() ([]byte, error) {
	type Alias BallotMode
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(b),
	})
}

// UnmarshalJSON implements json.Unmarshaler interface
func (b *BallotMode) UnmarshalJSON(data []byte) error {
	type Alias BallotMode
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(b),
	}
	return json.Unmarshal(data, aux)
}

// String returns a string representation of the BallotMode
func (b *BallotMode) String() string {
	data, err := json.Marshal(b)
	if err != nil {
		return ""
	}
	return string(data)
}
