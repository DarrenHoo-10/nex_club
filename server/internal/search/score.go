package search

import (
	"fmt"
	"math/big"
	"strings"
)

func canonicalScore(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	value := new(big.Rat)
	if _, ok := value.SetString(raw); !ok || value.Sign() < 0 {
		return "", fmt.Errorf("score")
	}
	scaled := new(big.Rat).Mul(value, big.NewRat(10000, 1))
	scaled.Add(scaled, big.NewRat(1, 2))
	whole := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	max := big.NewInt(100 * 10000)
	if whole.Cmp(max) > 0 || whole.Sign() < 0 {
		return "", fmt.Errorf("score")
	}
	v := whole.Int64()
	return fmt.Sprintf("%d.%04d", v/10000, v%10000), nil
}
