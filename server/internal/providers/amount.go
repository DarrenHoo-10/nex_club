package providers

import (
	"errors"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const amountScale = 8

// Amount is a non-negative decimal with scale 8. It never uses float64.
type Amount struct {
	u *big.Int
}

func (a Amount) norm() *big.Int {
	if a.u == nil {
		return big.NewInt(0)
	}
	return a.u
}

func (a Amount) Cmp(b Amount) int {
	return a.norm().Cmp(b.norm())
}

func (a Amount) Add(b Amount) Amount {
	return Amount{u: new(big.Int).Add(a.norm(), b.norm())}
}

func (a Amount) MulInt(n int64) Amount {
	if n < 0 {
		n = 0
	}
	return Amount{u: new(big.Int).Mul(a.norm(), big.NewInt(n))}
}

func (a Amount) String() string {
	s := a.norm().String()
	if len(s) <= amountScale {
		s = strings.Repeat("0", amountScale-len(s)) + s
		return "0." + s
	}
	i := len(s) - amountScale
	return s[:i] + "." + s[i:]
}

func (a Amount) Numeric() pgtype.Numeric {
	return pgtype.Numeric{Int: new(big.Int).Set(a.norm()), Exp: -amountScale, Valid: true}
}

// ParseAmount accepts a non-negative decimal with at most 8 fractional digits.
func ParseAmount(raw string) (Amount, error) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, "eE+ ") || strings.HasPrefix(s, "-") {
		return Amount{}, errors.New("invalid amount")
	}
	whole, frac, ok := strings.Cut(s, ".")
	if strings.Contains(frac, ".") {
		return Amount{}, errors.New("invalid amount")
	}
	if !ok {
		frac = ""
	}
	if whole == "" || frac == "" && ok {
		return Amount{}, errors.New("invalid amount")
	}
	if len(frac) > amountScale || !digits(whole) || !digits(frac) {
		return Amount{}, errors.New("invalid amount")
	}
	frac += strings.Repeat("0", amountScale-len(frac))
	u, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok || u.Sign() < 0 {
		return Amount{}, errors.New("invalid amount")
	}
	return Amount{u: u}, nil
}

func digits(s string) bool {
	if s == "" {
		return true
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func amountFromNumeric(n pgtype.Numeric) (Amount, error) {
	if !n.Valid || n.NaN || n.InfinityModifier != pgtype.Finite || n.Int == nil {
		return Amount{}, errors.New("invalid numeric")
	}
	if n.Int.Sign() < 0 {
		return Amount{}, errors.New("negative amount")
	}
	u := new(big.Int).Set(n.Int)
	shift := int64(n.Exp) + amountScale
	switch {
	case shift > 0:
		u.Mul(u, new(big.Int).Exp(big.NewInt(10), big.NewInt(shift), nil))
	case shift < 0:
		div := new(big.Int).Exp(big.NewInt(10), big.NewInt(-shift), nil)
		rem := new(big.Int)
		u.DivMod(u, div, rem)
		if rem.Sign() != 0 {
			return Amount{}, errors.New("numeric finer than scale 8")
		}
	}
	return Amount{u: u}, nil
}
