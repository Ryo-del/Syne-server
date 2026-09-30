package server

import (
	"crypto/rand"
	"errors"
	"math/big"
)

// без похожих символов (0/O, 1/I)
const claimAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randInt(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

func GenerateClaimCodeN(length int) (string, error) {
	if length < 1 {
		return "", errors.New("invalid length")
	}
	b := make([]byte, length)
	for i := range b {
		v, err := randInt(len(claimAlphabet))
		if err != nil {
			return "", err
		}
		b[i] = claimAlphabet[v]
	}
	return string(b), nil
}

// GenerateNumericID — число из digits цифр, первая не ноль.
func GenerateNumericID(digits int) (string, error) {
	if digits < 1 {
		return "", errors.New("invalid digits")
	}
	b := make([]byte, digits)
	for i := range b {
		v, err := randInt(10)
		if i == 0 {
			v, err = randInt(9)
			v++
		}
		if err != nil {
			return "", err
		}
		b[i] = byte('0' + v)
	}
	return string(b), nil
}
