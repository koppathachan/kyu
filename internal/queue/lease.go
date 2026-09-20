package queue

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// TokenVersion is the first byte of every lease token.
const TokenVersion byte = 1

// tokenHeaderSize is the fixed part: version, attempts, partition length.
const tokenHeaderSize = 1 + 4 + 2

// ErrBadToken is what every lease token decode failure reduces to.
var ErrBadToken = errors.New("lease token is not one this engine issued")

// Token is what a delivery hands the consumer so that it can settle it.
//
// It carries the attempt count the claim returned, which is the fence Delete
// and Release are given. The count travels in the token rather than in engine
// memory on purpose: that is what keeps an acknowledgement safe across an
// engine restart and across engine instances, neither of which shares memory
// with the instance that granted the hold.
//
// It is opaque to the consumer and useful for nothing but settling the one
// delivery it names.
type Token struct {
	Partition string
	ID        string
	Attempts  uint32
}

// Encode renders the token as the bytes that travel as Message.lease_token.
func (t Token) Encode() []byte {
	p := []byte(t.Partition)
	b := make([]byte, tokenHeaderSize+len(p)+len(t.ID))
	b[0] = TokenVersion
	binary.BigEndian.PutUint32(b[1:5], t.Attempts)
	binary.BigEndian.PutUint16(b[5:7], uint16(len(p)))
	copy(b[tokenHeaderSize:], p)
	copy(b[tokenHeaderSize+len(p):], t.ID)
	return b
}

// DecodeToken reads back a token this engine, or one of its peers, issued.
//
// A token that will not decode is a client error rather than a lost race: it
// was never issued by any engine serving this contract.
func DecodeToken(b []byte) (Token, error) {
	if len(b) < tokenHeaderSize {
		return Token{}, fmt.Errorf("%w: %d bytes is shorter than the header", ErrBadToken, len(b))
	}
	if b[0] != TokenVersion {
		return Token{}, fmt.Errorf("%w: version %d, this engine reads version %d", ErrBadToken, b[0], TokenVersion)
	}
	attempts := binary.BigEndian.Uint32(b[1:5])
	plen := int(binary.BigEndian.Uint16(b[5:7]))
	if len(b) < tokenHeaderSize+plen {
		return Token{}, fmt.Errorf("%w: partition length %d exceeds the token", ErrBadToken, plen)
	}
	t := Token{
		Partition: string(b[tokenHeaderSize : tokenHeaderSize+plen]),
		ID:        string(b[tokenHeaderSize+plen:]),
		Attempts:  attempts,
	}
	if t.Partition == "" || t.ID == "" {
		return Token{}, fmt.Errorf("%w: it names no partition or no message", ErrBadToken)
	}
	return t, nil
}
